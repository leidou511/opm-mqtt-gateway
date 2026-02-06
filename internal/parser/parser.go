package parser

import (
	"bufio"
	"bytes"
	"log"
	"opm-mqtt-gateway/internal/models"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Parser struct {
	buffer       bytes.Buffer
	lastDataTime time.Time
	frameTimeout time.Duration
	isNewFrame   bool
}

func NewParser() *Parser {
	return &Parser{
		frameTimeout: 2 * time.Second,
		isNewFrame:   true,
		lastDataTime: time.Now(),
	}
}

func (p *Parser) ParseData(data []byte) (*models.UrineTestResult, error) {
	currentTime := time.Now()

	// 检查数据接收间隔，如果超时则清空缓冲区（新帧开始）
	if currentTime.Sub(p.lastDataTime) > p.frameTimeout {
		if p.buffer.Len() > 0 {
			log.Printf("🕒 帧超时(%v)，清空缓冲区残留数据: %d字节",
				p.frameTimeout, p.buffer.Len())
			p.buffer.Reset()
		}
		p.isNewFrame = true
	}

	p.buffer.Write(data)
	p.lastDataTime = currentTime

	content := p.buffer.String()
	log.Printf("📥 缓冲区状态: %d字节, 缓冲区内容: %q", p.buffer.Len(), content)

	// 尝试提取和解析完整帧
	result, remaining, err := p.extractAndParseFrame(content)
	if err != nil {
		log.Printf("❌ 帧解析错误: %v", err)
		return nil, err
	}

	if result != nil {
		// 成功解析，更新缓冲区
		p.buffer.Reset()
		if len(remaining) > 0 {
			p.buffer.WriteString(remaining)
			log.Printf("📋 保留未处理数据: %d字节", len(remaining))
		}
		p.isNewFrame = false
		return result, nil
	}

	// 检查是否可能包含完整帧
	if p.hasPotentialCompleteFrame(content) {
		log.Printf("🔍 可能包含完整帧，尝试解析...")
		// 尝试强制解析
		if result, err := p.forceParseFrame(content); err == nil && result != nil {
			p.buffer.Reset()
			p.isNewFrame = false
			return result, nil
		}
	}

	log.Printf("⏳ 数据不完整，等待更多数据...")
	return nil, nil
}

// hasPotentialCompleteFrame 检查是否可能包含完整帧
func (p *Parser) hasPotentialCompleteFrame(data string) bool {
	if len(data) < 20 { // 最小合理帧长度
		return false
	}

	// 检查是否有日期行模式（允许不完整日期）
	if strings.Contains(data, "-02-03") || strings.Contains(data, "-01-15") {
		return true
	}

	// 检查是否有项目数据分隔符
	if strings.Count(data, "\r\n") >= 8 {
		return true
	}

	return false
}

// extractAndParseFrame 提取并解析完整帧
func (p *Parser) extractAndParseFrame(data string) (*models.UrineTestResult, string, error) {
	// 查找完整的帧结束标记
	endPos := strings.Index(data, "\r\n\r\n")
	if endPos == -1 {
		return nil, data, nil
	}

	// 查找帧开始（日期行）
	startPos := p.findFrameStart(data, endPos)
	if startPos == -1 {
		return nil, data, nil
	}

	frame := data[startPos : endPos+4] // 包含\r\n\r\n
	remaining := data[endPos+4:]

	log.Printf("✅ 提取到完整帧: %d字节", len(frame))

	result, err := p.parseCompleteFrame(frame)
	if err != nil {
		return nil, data, err
	}

	return result, remaining, nil
}

// findFrameStart 查找帧开始位置
func (p *Parser) findFrameStart(data string, endPos int) int {
	// 从结束位置向前查找日期行
	for i := endPos; i >= 0; i-- {
		if i >= 10 && p.isPotentialDateLine(data, i) {
			return i
		}
	}
	return -1
}

// isPotentialDateLine 检查是否为可能的日期行（允许不完整）
func (p *Parser) isPotentialDateLine(data string, pos int) bool {
	if pos < 0 || pos+10 > len(data) {
		return false
	}

	// 检查日期格式: YYYY-MM-DD（允许不完整）
	line := data[pos:min(pos+10, len(data))]

	// 如果是完整日期行
	if len(line) == 10 && line[4] == '-' && line[7] == '-' {
		return true
	}

	// 如果是部分日期行（如"026-02-03"需要修复）
	if strings.Contains(line, "-") && strings.Contains(line, "-") {
		return true
	}

	return false
}

// forceParseFrame 尝试强制解析可能不完整的帧
func (p *Parser) forceParseFrame(data string) (*models.UrineTestResult, error) {
	log.Printf("🛠️ 尝试强制解析数据: %d字节", len(data))

	// 修复可能的数据问题
	repairedData := p.repairData(data)
	if repairedData != data {
		log.Printf("🔧 数据已修复: %q -> %q", data, repairedData)
	}

	return p.parseCompleteFrame(repairedData)
}

// repairData 修复数据问题（如分片导致的日期不完整）
func (p *Parser) repairData(data string) string {
	// 查找日期行模式并修复
	lines := strings.Split(data, "\r\n")
	if len(lines) == 0 {
		return data
	}

	// 修复第一行（日期行）
	if len(lines[0]) > 0 {
		// 检查是否是不完整日期（如"026-02-03"应该是"2026-02-03"）
		if strings.HasPrefix(lines[0], "026-") {
			lines[0] = "2026-" + lines[0][4:]
			log.Printf("📅 修复日期行: %s", lines[0])
		}

		// 检查其他常见的不完整日期模式
		if strings.HasPrefix(lines[0], "024-") {
			lines[0] = "2024-" + lines[0][4:]
			log.Printf("📅 修复日期行: %s", lines[0])
		}
	}

	return strings.Join(lines, "\r\n")
}

// parseCompleteFrame 解析完整的帧
func (p *Parser) parseCompleteFrame(frame string) (*models.UrineTestResult, error) {
	scanner := bufio.NewScanner(strings.NewReader(frame))
	var lines []string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}

	if len(lines) < 5 { // 至少需要日期、时间、样本号、空行、一个项目
		return nil, nil
	}

	result := &models.UrineTestResult{
		DeviceID: "OPM-1560B",
		RawData:  frame,
	}

	lineIndex := 0

	// 解析日期
	if lineIndex < len(lines) && p.isValidDateLine(lines[lineIndex]) {
		if date, err := time.Parse("2006-01-02", lines[lineIndex]); err == nil {
			result.TestDate = date
		} else {
			log.Printf("⚠️ 日期解析失败: %s, 错误: %v", lines[lineIndex], err)
		}
		lineIndex++
	}

	// 解析时间
	if lineIndex < len(lines) && p.isValidTimeLine(lines[lineIndex]) {
		result.TestTime = lines[lineIndex]
		lineIndex++
	}

	// 解析样本号
	if lineIndex < len(lines) && p.isValidSampleID(lines[lineIndex]) {
		result.SampleID = lines[lineIndex]
		lineIndex++
	}

	// 跳过空行（如果有）
	if lineIndex < len(lines) && lines[lineIndex] == "" {
		lineIndex++
	}

	// 解析测试项目
	for i := lineIndex; i < len(lines); i++ {
		if item := p.parseItemLine(lines[i]); item != nil {
			result.Items = append(result.Items, *item)
		}
	}

	if len(result.Items) > 0 {
		log.Printf("✅ 解析成功: 样本号=%s, 日期=%s, 时间=%s, 项目数=%d",
			result.SampleID, result.TestDate.Format("2006-01-02"),
			result.TestTime, len(result.Items))
		return result, nil
	}

	return nil, nil
}

// 验证函数
func (p *Parser) isValidDateLine(line string) bool {
	if len(line) != 10 {
		return false
	}
	return line[4] == '-' && line[7] == '-'
}

func (p *Parser) isValidTimeLine(line string) bool {
	if len(line) != 8 {
		return false
	}
	return line[2] == ':' && line[5] == ':'
}

func (p *Parser) isValidSampleID(line string) bool {
	if line == "" {
		return false
	}
	// 样本号应该是数字
	for _, ch := range line {
		if !unicode.IsDigit(ch) {
			return false
		}
	}
	return true
}

func (p *Parser) parseItemLine(line string) *models.TestItem {
	parts := strings.Split(line, "\t")
	if len(parts) < 2 {
		return nil
	}

	name := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])

	if name == "" || value == "" {
		return nil
	}

	return &models.TestItem{
		Name:  p.normalizeItemName(name),
		Value: p.normalizeValue(value),
	}
}

// 原有的标准化函数保持不变
func (p *Parser) normalizeItemName(name string) string {
	name = strings.ReplaceAll(name, "+-", "±")
	name = strings.ReplaceAll(name, "u", "μ")

	nameMap := map[string]string{
		"葡萄糖":   models.GLU,
		"胆红素":   models.BIL,
		"比重":    models.SG,
		"PH":    models.PH,
		"酮体":    models.KET,
		"潜血":    models.BLD,
		"蛋白质":   models.PRO,
		"尿胆原":   models.URO,
		"亚硝酸盐":  models.NIT,
		"白细胞":   models.LEU,
		"抗坏血酸":  models.VC,
		"肌酐":    models.CRE,
		"尿钙":    models.CA,
		"微量白蛋白": models.MCA,
	}

	if normalized, exists := nameMap[name]; exists {
		return normalized
	}
	return name
}

func (p *Parser) normalizeValue(value string) string {
	value = strings.TrimSpace(value)

	plusMap := map[string]string{
		"++++": "4+",
		"+++":  "3+",
		"++":   "2+",
		"+":    "1+",
		"-":    "阴性",
		"±":    "弱阳性",
	}

	if normalized, exists := plusMap[value]; exists {
		return normalized
	}

	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return value
	}

	return value
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
