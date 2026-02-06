package parser

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"opm-mqtt-gateway/internal/config"
	"opm-mqtt-gateway/internal/models"
)

// Parser OPM-1560B协议解析器实例（贴合硬件帧格式+数据编码，核心层）
type Parser struct {
	frameStart  []byte // 帧头（0xAA）
	frameEnd    []byte // 帧尾（0x55）
	checkType   string // 校验方式（sum，和校验）
	minFrameLen int    // 最小帧长度（16字节）
	deviceID    string // 设备SN
	deviceModel string // 设备型号（OPM-1560B）
}

// NewParser 新建解析器实例（基于全局硬件配置初始化）
func NewParser() *Parser {
	cfg := config.GlobalConfig
	return &Parser{
		frameStart:  config.GetFrameStart(),
		frameEnd:    config.GetFrameEnd(),
		checkType:   cfg.Parser.CheckType,
		minFrameLen: cfg.Parser.FrameMinLen,
		deviceID:    cfg.Device.DeviceID,
		deviceModel: cfg.Device.Model,
	}
}

// Parse 核心：解析OPM-1560B有效帧，流程：三重校验→数据提取→编码解析→模型映射
func (p *Parser) Parse(frame []byte) (*models.OPM1560BDeviceData, error) {
	// 1. 第一重校验：帧长度（硬件约束，不足16字节直接丢弃）
	if len(frame) < p.minFrameLen {
		return nil, fmt.Errorf("帧长度不足，实际%d，要求%d", len(frame), p.minFrameLen)
	}

	// 2. 第二重校验：帧头/帧尾（硬件约束，AA开头/55结尾）
	startLen, endLen := len(p.frameStart), len(p.frameEnd)
	if !p.compareBytes(frame[:startLen], p.frameStart) {
		return nil, errors.New("帧头校验失败（非AA）")
	}
	if !p.compareBytes(frame[len(frame)-endLen:], p.frameEnd) {
		return nil, errors.New("帧尾校验失败（非55）")
	}

	// 3. 提取校验位和原始帧（硬件格式：AA+数据段+校验位+55）
	checkSum := frame[len(frame)-endLen-1] // 校验位在帧尾前1字节
	serialFrame := models.NewSerialFrame(frame, p.frameStart, p.frameEnd, checkSum)

	// 4. 第三重校验：和校验（硬件固化算法，数据段字节和取低8位）
	if p.checkType == models.CheckTypeSum {
		if !p.checkSumValid(serialFrame.Data, checkSum) {
			calcSum := p.calcSum(serialFrame.Data)
			log.Printf("[ERROR] [parser] 和校验失败，计算值0x%02X，帧中值0x%02X，原始帧%s",
				calcSum, checkSum, models.HexStr(frame))
			return nil, errors.New("和校验失败")
		}
	}

	log.Printf("[INFO] [parser] 帧校验通过，数据段长度%d，原始帧%s",
		len(serialFrame.Data), models.HexStr(frame))

	// 5. 核心：从数据段提取检测数据（硬件数据段字节分布精准映射）
	deviceData, err := p.extractDetectData(serialFrame.Data)
	if err != nil {
		return nil, fmt.Errorf("提取数据失败：%w", err)
	}

	// 6. 留存原始帧16进制（调试/溯源）
	deviceData.RawFrameHex = strings.ToUpper(hex.EncodeToString(frame))
	// 7. 校验数据医学有效性，标记状态
	deviceData.CheckDataValid()

	return deviceData, nil
}

// checkSumValid 验证和校验是否有效（OPM-1560B硬件固化算法）
func (p *Parser) checkSumValid(data []byte, frameCheckSum byte) bool {
	return p.calcSum(data) == frameCheckSum
}

// calcSum 计算和校验（硬件算法：数据段所有字节相加，结果取低8位）
func (p *Parser) calcSum(data []byte) byte {
	var sum uint16
	for _, b := range data {
		sum += uint16(b)
	}
	return byte(sum & 0xFF) // 取低8位
}

// extractDetectData 核心：从硬件数据段提取检测数据（字节分布与OPM-1560B完全一致）
// 硬件数据段规范（共14字节，固化不可改）：
// 字节0-1：PH值（BCD码，如0x0520 → 5.20）
// 字节2：尿蛋白（0:-/1:+ /2:± /3:++ /4:+++ /5:++++）
// 字节3：葡萄糖（编码同尿蛋白）
// 字节4：酮体（编码同尿蛋白）
// 字节5：隐血（编码同尿蛋白）
// 字节6：白细胞（编码同尿蛋白）
// 字节7：红细胞（编码同尿蛋白）
// 字节8：尿胆原（编码同尿蛋白）
// 字节9：胆红素（编码同尿蛋白）
// 字节10：亚硝酸盐（0:-/1:+）
// 字节11-12：比重（BCD码，如0x1010 → 1.010）
// 字节13：维生素C（编码同尿蛋白）
func (p *Parser) extractDetectData(data []byte) (*models.OPM1560BDeviceData, error) {
	// 初始化检测数据模型
	deviceData := models.NewOPM1560BDeviceData(p.deviceID, p.deviceModel)

	// 数据段长度校验（硬件约束14字节，不足则解析失败）
	if len(data) < 14 {
		return nil, fmt.Errorf("数据段长度不足，实际%d，要求14", len(data))
	}

	// 1. 解析PH值（BCD码：字节0-1 → 浮点数）
	phBCD := (uint16(data[0]) << 8) | uint16(data[1])
	phStr := fmt.Sprintf("%04d", phBCD)
	ph, err := strconv.ParseFloat(phStr[:1]+"."+phStr[1:], 64)
	if err != nil {
		return nil, fmt.Errorf("解析PH值失败：%w", err)
	}
	deviceData.PH = ph

	// 2. 解析等级型检测项（硬件编码：0-5对应-/+/±/++/+++/++++）
	deviceData.Protein = p.parseGrade(data[2])      // 尿蛋白
	deviceData.Glucose = p.parseGrade(data[3])      // 葡萄糖
	deviceData.Ketone = p.parseGrade(data[4])       // 酮体
	deviceData.OccultBlood = p.parseGrade(data[5])  // 隐血
	deviceData.Leukocyte = p.parseGrade(data[6])    // 白细胞
	deviceData.Erythrocyte = p.parseGrade(data[7])  // 红细胞
	deviceData.Urobilinogen = p.parseGrade(data[8]) // 尿胆原
	deviceData.Bilirubin = p.parseGrade(data[9])    // 胆红素
	deviceData.VC = p.parseGrade(data[13])          // 维生素C

	// 3. 解析亚硝酸盐（硬件编码：0:-/1:+）
	switch data[10] {
	case 0:
		deviceData.Nitrite = "-"
	case 1:
		deviceData.Nitrite = "+"
	default:
		deviceData.Nitrite = "invalid"
	}

	// 4. 解析比重（BCD码：字节11-12 → 浮点数）
	sgBCD := (uint16(data[11]) << 8) | uint16(data[12])
	sgStr := fmt.Sprintf("%04d", sgBCD)
	sg, err := strconv.ParseFloat(sgStr[:1]+"."+sgStr[1:], 64)
	if err != nil {
		return nil, fmt.Errorf("解析比重失败：%w", err)
	}
	deviceData.SpecificGrav = sg

	return deviceData, nil
}

// parseGrade 解析硬件等级编码（OPM-1560B固化编码规则）
func (p *Parser) parseGrade(b byte) string {
	switch b {
	case 0:
		return "-"
	case 1:
		return "+"
	case 2:
		return "±"
	case 3:
		return "++"
	case 4:
		return "+++"
	case 5:
		return "++++"
	default:
		return "invalid"
	}
}

// compareBytes 工具方法：比较字节数组是否相等（帧头/帧尾匹配）
func (p *Parser) compareBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
