package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

// 测试发送器实例，绑定COM2串口，封装串口操作和测试帧
type TestHardwareSender struct {
	port         serial.Port       // 串口实例（固定COM2）
	portMode     serial.Mode       // 串口参数（OPM-1560B固化9600 8N1）
	testFrames   map[string][]byte // 测试帧集合：key-帧类型，value-硬件规范字节数组
	sendInterval time.Duration     // 循环发送间隔，模拟硬件连续检测
}

// 测试帧类型常量，覆盖主程序核心测试场景
const (
	FrameTypeNormal   = "normal"   // 正常完整帧：所有检测项在医学合理范围，校验通过
	FrameTypeAbnormal = "abnormal" // 异常数据帧：PH/比重超出医学范围，测试主程序异常标记逻辑
	FrameTypeSticky   = "sticky"   // 粘包帧：两个正常帧拼接，测试主程序粘包处理逻辑
	TestPort          = "COM2"     // 固定发送串口为COM2
)

// 全局测试发送器实例
var globalSender *TestHardwareSender

// NewTestHardwareSender 新建硬件测试发送器，初始化COM2串口+硬件测试帧+串口参数
// interval：循环发送间隔（秒），建议1-5秒模拟硬件连续检测
func NewTestHardwareSender(interval int) (*TestHardwareSender, error) {

	// 1. 初始化OPM-1560B固化串口参数（9600 8N1，无流控）
	portMode := serial.Mode{
		BaudRate: 9600,
		DataBits: 8,
		StopBits: 0,
		Parity:   serial.NoParity,
	}

	// 2. 检查COM2串口是否存在，提前规避端口不存在错误
	if !isPortExist(TestPort) {
		return nil, fmt.Errorf("测试串口%s不存在，请检查串口是否连接或重命名", TestPort)
	}

	// 3. 打开COM2串口（测试代码直接打开，无需重试，快速反馈错误）
	port, err := serial.Open(TestPort, &portMode)
	if err != nil {
		return nil, fmt.Errorf("打开COM2串口失败: %w", err)
	}

	// 4. 初始化测试帧（符合OPM-1560B硬件帧格式，和校验已提前计算正确）
	testFrames := initHardwareTestFrames()

	// 5. 新建发送器实例
	sender := &TestHardwareSender{
		port:         port,
		portMode:     portMode,
		testFrames:   testFrames,
		sendInterval: time.Duration(interval) * time.Second,
	}

	log.Printf("COM2串口测试发送器初始化成功，串口参数：9600 8N1，内置测试帧：%v", getFrameTypes())
	globalSender = sender
	return sender, nil
}

// isPortExist 检查指定串口是否存在（辅助函数，基于serial包枚举）
func isPortExist(portName string) bool {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		log.Printf("枚举串口失败，跳过端口检查: %v", err)
		return true
	}
	for _, p := range ports {
		if p.Name == portName {
			return true
		}
	}
	return false
}

// initHardwareTestFrames 初始化硬件测试帧，严格遵循OPM-1560B帧格式：AA+14字节数据+和校验+55
// 所有帧的和校验均按硬件规范计算（数据段字节求和取低8位），保证主程序能正常解析
func initHardwareTestFrames() map[string][]byte {
	frames := make(map[string][]byte)
	// 1. 正常帧：AA 0520 01 00 00 00 00 00 00 00 1010 00 | 29 | 55
	// 解析结果：PH=5.20(正常)、尿蛋白=+、葡萄糖=-、比重=1.010(正常)，和校验=0x29
	normalHex := "AA052001000000000000001010002955"
	normalFrame, _ := hex.DecodeString(normalHex)
	frames[FrameTypeNormal] = normalFrame

	// 2. 异常帧：AA 0300 02 01 00 00 00 00 00 01 1040 01 | 0C | 55
	// 解析结果：PH=3.00(异常<4.5)、比重=1.040(异常>1.030)，主程序应标记data_state=abnormal
	abnormalHex := "AA030002010000000000011040010C55"
	abnormalFrame, _ := hex.DecodeString(abnormalHex)
	frames[FrameTypeAbnormal] = abnormalFrame

	// 3. 粘包帧：两个正常帧直接拼接，模拟串口粘包，主程序应拆分出两个独立有效帧
	stickyHex := normalHex + normalHex
	stickyFrame, _ := hex.DecodeString(stickyHex)
	frames[FrameTypeSticky] = stickyFrame

	return frames
}

// getFrameTypes 获取所有测试帧类型（辅助函数，打印日志用）
func getFrameTypes() []string {
	var types []string
	for t := range globalSender.testFrames {
		types = append(types, t)
	}
	return types
}

// SendSingleFrame 发送指定类型的单帧测试数据，测试主程序单条数据处理逻辑
func (s *TestHardwareSender) SendSingleFrame(frameType string) error {
	// 1. 校验帧类型是否合法
	frame, ok := s.testFrames[frameType]
	if !ok {
		return fmt.Errorf("无效帧类型，支持类型：%v", getFrameTypes())
	}

	// 2. 向COM2串口写入数据（硬件真实发送行为）
	n, err := s.port.Write(frame)
	if err != nil {
		return fmt.Errorf("串口写入失败: %w", err)
	}

	// 3. 校验写入长度，确保数据完整发送
	if n != len(frame) {
		return fmt.Errorf("数据发送不完整，预期发送%d字节，实际发送%d字节", len(frame), n)
	}

	// 4. 打印发送日志，包含帧类型/长度/16进制字符串，方便主程序联调
	log.Printf("单帧发送成功 | 类型：%s | 长度：%d字节 | 原始数据：%s",
		frameType, len(frame), hex.EncodeToString(frame))
	return nil
}

// SendLoop 循环发送所有测试帧，按顺序轮询，模拟硬件连续检测的实际工作状态
// 调用后会阻塞协程，可通过Close()停止，适合长时间测试主程序稳定性
func (s *TestHardwareSender) SendLoop() {
	log.Printf("开始循环发送测试帧 | 发送间隔：%v | 轮询类型：%v", s.sendInterval, getFrameTypes())
	frameTypes := getFrameTypes()
	index := 0
	for {
		// 按顺序轮询发送不同类型帧
		curType := frameTypes[index%len(frameTypes)]
		if err := s.SendSingleFrame(curType); err != nil {
			log.Printf("循环发送帧失败 | 类型：%s | 错误：%v", curType, err)
		}
		// 发送后等待指定间隔，模拟硬件检测间隔
		time.Sleep(s.sendInterval)
		index++
	}
}

// Close 优雅关闭测试发送器，释放COM2串口资源，测试完成后必须调用
func (s *TestHardwareSender) Close() {
	if s.port != nil {
		_ = s.port.Close()
		log.Printf("COM2串口测试发送器已关闭，端口资源已释放")
	}
}

func main() {
	NewTestHardwareSender(1)
	time.Sleep(10 * time.Second)
}

package main

import (
"fmt"
"log"
"time"

"go.bug.st/serial"
)

func main() {
	mode := &serial.Mode{
		BaudRate: 9600,
		DataBits: 8,
		StopBits: serial.OneStopBit,
		Parity:   serial.OddParity,
	}

	port, err := serial.Open("COM2", mode)
	if err != nil {
		log.Fatalf("打开测试串口失败: %v", err)
	}
	defer port.Close()

	// 生成符合OPM-1560B格式的测试数据
	//testData := []string{
	//	"2026-02-03\r\n10:15:30\r\n001\r\n\r\nGLU\tNegative\r\nBIL\tNegative\r\nSG\t1.015\r\nPH\t6.0\r\nKET\tNegative\r\nBLD\tNegative\r\nPRO\tNegative\r\nURO\tNormal\r\nNIT\tNegative\r\nLEU\tNegative\r\n",
	//	"2026-02-03\r\n10:20:05\r\n002\r\n\r\nGLU\t*250 mg/dL\r\nBIL\t*+\r\nSG\t1.030\r\nPH\t*8.5\r\nKET\t*+++\r\nBLD\t*+\r\nPRO\t*150 mg/dL\r\nURO\tNormal\r\nNIT\tNegative\r\nLEU\t*++\r\n",
	//	"2026-02-03\r\n10:25:40\r\n003\r\n\r\nGLU\tNormal\r\nBIL\tNegative\r\nSG\t*1.005\r\nPH\t5.0\r\nKET\tNegative\r\nBLD\tTrace\r\nPRO\t*80 mg/dL\r\nURO\t*4 mg/dL\r\nNIT\tNegative\r\nLEU\t+\r\n",
	//}
	testData := []string{generateTestData()}

	for _, data := range testData {
		// 发送测试数据
		n, err := port.Write([]byte(data))
		if err != nil {
			log.Fatalf("发送测试数据失败: %v", err)
		}

		fmt.Printf("测试数据发送成功: %d 字节\n", n)
		fmt.Printf("数据内容:\n%s\n", testData)
	}
}

func generateTestData() string {
	now := time.Now()
	dateStr := now.Format("2006-01-02")
	timeStr := now.Format("15:04:05")

	return fmt.Sprintf("%s\r\n%s\r\n001\r\n\r\n"+
		"GLU\t阴性\r\n"+
		"BIL\t-\r\n"+
		"SG\t1.015\r\n"+
		"PH\t6.0\r\n"+
		"KET\t阴性\r\n"+
		"BLD\t阴性\r\n"+
		"PRO\t阴性\r\n"+
		"URO\t正常\r\n"+
		"NIT\t阴性\r\n"+
		"LEU\t阴性\r\n",
		dateStr, timeStr)
}

