package models

import "time"

// UrineTestResult 尿液分析结果
type UrineTestResult struct {
	DeviceID string     `json:"device_id"`
	SampleID string     `json:"sample_id"`
	TestDate time.Time  `json:"test_date"`
	TestTime string     `json:"test_time"`
	Items    []TestItem `json:"items"`
	RawData  string     `json:"raw_data,omitempty"`
}

// TestItem 测试项目
type TestItem struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
}

// 测试项目常量
const (
	GLU = "GLU" // 葡萄糖
	BIL = "BIL" // 胆红素
	SG  = "SG"  // 比重
	PH  = "PH"  // PH值
	KET = "KET" // 酮体
	BLD = "BLD" // 潜血
	PRO = "PRO" // 蛋白质
	URO = "URO" // 尿胆原
	NIT = "NIT" // 亚硝酸盐
	LEU = "LEU" // 白细胞
	VC  = "VC"  // 抗坏血酸
	CRE = "CRE" // 肌酐
	CA  = "CA"  // 尿钙
	MCA = "MCA" // 微量白蛋白
)
