package parser

import (
	"opm-mqtt-gateway/internal/models"
	"testing"
	"time"
)

func TestParser_ParseCompleteFrame(t *testing.T) {
	parser := NewParser()

	// 完整的10项试纸条数据帧
	testData := "2024-01-15\r\n14:30:25\r\n001\r\n\r\n" +
		"GLU\t阴性\r\n" +
		"BIL\t-\r\n" +
		"SG\t1.015\r\n" +
		"PH\t6.0\r\n" +
		"KET\t阴性\r\n" +
		"BLD\t阴性\r\n" +
		"PRO\t阴性\r\n" +
		"URO\t正常\r\n" +
		"NIT\t阴性\r\n" +
		"LEU\t阴性\r\n"

	result, err := parser.ParseData([]byte(testData))
	if err != nil {
		t.Fatalf("解析完整数据帧失败: %v", err)
	}

	if result == nil {
		t.Fatal("解析结果为空")
	}

	if result.SampleID != "001" {
		t.Errorf("样本号解析错误: 期望=001, 实际=%s", result.SampleID)
	}

	expectedDate, _ := time.Parse("2006-01-02", "2024-01-15")
	if !result.TestDate.Equal(expectedDate) {
		t.Errorf("日期解析错误: 期望=%v, 实际=%v", expectedDate, result.TestDate)
	}

	if result.TestTime != "14:30:25" {
		t.Errorf("时间解析错误: 期望=14:30:25, 实际=%s", result.TestTime)
	}

	if len(result.Items) != 10 {
		t.Errorf("项目数量错误: 期望=10, 实际=%d", len(result.Items))
	}
}

func TestParser_ParseChineseItemNames(t *testing.T) {
	parser := NewParser()

	// 测试中文项目名称
	testData := "2024-01-16\r\n09:15:30\r\n002\r\n\r\n" +
		"葡萄糖\t阴性\r\n" +
		"胆红素\t-\r\n" +
		"比重\t1.020\r\n" +
		"PH\t7.0\r\n"

	result, err := parser.ParseData([]byte(testData))
	if err != nil {
		t.Fatalf("解析中文项目名称失败: %v", err)
	}

	if result == nil {
		t.Fatal("解析结果为空")
	}

	// 验证中文项目名称映射
	expectedMappings := map[string]string{
		"葡萄糖": models.GLU,
		"胆红素": models.BIL,
		"比重":  models.SG,
	}

	for _, item := range result.Items {
		if mappedName, exists := expectedMappings[item.Name]; exists {
			if item.Name != mappedName {
				t.Errorf("中文项目名称映射错误: 原始=%s, 期望=%s, 实际=%s",
					item.Name, mappedName, item.Name)
			}
		}
	}
}

func TestParser_ParseGradientValues(t *testing.T) {
	parser := NewParser()

	// 测试梯度单位值
	testData := "2024-01-17\r\n11:20:15\r\n003\r\n\r\n" +
		"GLU\t阴性\r\n" +
		"BLD\t+\r\n" +
		"PRO\t++\r\n" +
		"KET\t+++\r\n" +
		"LEU\t++++\r\n"

	result, err := parser.ParseData([]byte(testData))
	if err != nil {
		t.Fatalf("解析梯度单位值失败: %v", err)
	}

	if result == nil {
		t.Fatal("解析结果为空")
	}

	// 验证梯度单位标准化
	gradientMap := map[string]string{
		"+":    "1+",
		"++":   "2+",
		"+++":  "3+",
		"++++": "4+",
	}

	for _, item := range result.Items {
		if expectedValue, exists := gradientMap[item.Value]; exists {
			if item.Value != expectedValue {
				t.Errorf("梯度单位标准化错误: 项目=%s, 期望=%s, 实际=%s",
					item.Name, expectedValue, item.Value)
			}
		}
	}
}
