package parser

import (
	"encoding/hex"
	"testing"

	"opm-mqtt-gateway/internal/config"
	"opm-mqtt-gateway/internal/models"
)

// init 模拟全局配置初始化（单元测试无需加载配置文件，直接模拟硬件参数）
func init() {
	config.GlobalConfig = &config.Config{
		Device: config.DeviceConfig{
			DeviceID: "SN1234567890", // 测试设备SN
			Model:    "OPM-1560B",
		},
		Parser: config.ParserConfig{
			FrameStart:  "AA",
			FrameEnd:    "55",
			CheckType:   "sum",
			FrameMinLen: 16,
		},
	}
}

// TestParse_NormalFrame 测试：正常帧解析（OPM-1560B真实硬件帧）
// 帧：AA 0520 01 00 00 00 00 00 00 00 1010 00 29 55
// 预期：PH=5.20，尿蛋白=+，葡萄糖=-，比重=1.010，和校验=0x29，数据状态normal
func TestParse_NormalFrame(t *testing.T) {
	frameHex := "AA052001000000000000001010002955"
	frame, _ := hex.DecodeString(frameHex)

	parser := NewParser()
	data, err := parser.Parse(frame)
	if err != nil {
		t.Fatalf("正常帧解析失败：%v", err)
	}

	// 断言PH值
	if data.PH != 5.20 {
		t.Errorf("PH解析错误，预期5.20，实际%.2f", data.PH)
	}
	// 断言尿蛋白
	if data.Protein != "+" {
		t.Errorf("尿蛋白解析错误，预期+，实际%s", data.Protein)
	}
	// 断言比重
	if data.SpecificGrav != 1.010 {
		t.Errorf("比重解析错误，预期1.010，实际%.3f", data.SpecificGrav)
	}
	// 断言数据状态
	if data.DataState != models.DataStateNormal {
		t.Errorf("数据状态错误，预期normal，实际%s", data.DataState)
	}

	t.Logf("正常帧解析成功，数据：%+v", data)
}

// TestParse_CheckSumError 测试：和校验失败帧（硬件常见异常，应解析失败）
func TestParse_CheckSumError(t *testing.T) {
	// 校验位改为0x99，其余与正常帧一致
	frameHex := "AA052001000000000000001010009955"
	frame, _ := hex.DecodeString(frameHex)

	parser := NewParser()
	_, err := parser.Parse(frame)
	if err == nil {
		t.Fatal("和校验失败帧未返回错误，不符合预期")
	}
	if err.Error() != "和校验失败" {
		t.Errorf("错误类型错误，预期和校验失败，实际%v", err)
	}
	t.Logf("和校验失败帧解析符合预期，错误：%v", err)
}

// TestParse_FrameHeaderError 测试：帧头错误帧（非AA，应解析失败）
func TestParse_FrameHeaderError(t *testing.T) {
	// 帧头改为0xBB，其余与正常帧一致
	frameHex := "BB052001000000000000001010002955"
	frame, _ := hex.DecodeString(frameHex)

	parser := NewParser()
	_, err := parser.Parse(frame)
	if err == nil {
		t.Fatal("帧头错误帧未返回错误，不符合预期")
	}
	if err.Error() != "帧头校验失败（非AA）" {
		t.Errorf("错误类型错误，预期帧头校验失败，实际%v", err)
	}
	t.Logf("帧头错误帧解析符合预期，错误：%v", err)
}

// TestParse_AbnormalData 测试：异常数据帧（PH=3.00超出医学范围，应标记abnormal）
func TestParse_AbnormalData(t *testing.T) {
	// PH=3.00（BCD码0x0300），其余与正常帧一致，和校验=0x0C
	frameHex := "AA030001000000000000001010000C55"
	frame, _ := hex.DecodeString(frameHex)

	parser := NewParser()
	data, err := parser.Parse(frame)
	if err != nil {
		t.Fatalf("异常数据帧解析失败：%v", err)
	}
	// 断言数据状态为abnormal
	if data.DataState != models.DataStateAbnormal {
		t.Errorf("数据状态错误，预期abnormal，实际%s", data.DataState)
	}
	t.Logf("异常数据帧解析成功，数据状态：%s", data.DataState)
}
