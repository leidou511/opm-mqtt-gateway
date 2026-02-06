package models

import (
	"encoding/json"
	"time"
)

// 全局常量（OPM-1560B硬件/协议固化，统一管理，避免硬编码）
const (
	// 校验方式
	CheckTypeSum = "sum"
	// MQTT消息类型
	MQTTMsgTypeData  = "data"  // 检测数据上报
	MQTTMsgTypeState = "state" // 设备状态上报
	// 设备运行状态
	DeviceStateOnline  = "online"
	DeviceStateOffline = "offline"
	DeviceStateError   = "error"
	// 检测数据状态（医用分级）
	DataStateNormal   = "normal"   // 正常（值在医学合理范围）
	DataStateAbnormal = "abnormal" // 异常（值超出范围）
	DataStateInvalid  = "invalid"  // 无效（解析/校验失败）
	// 医学合理范围（OPM-1560B检测项参考）
	PHMin, PHMax                     = 4.5, 8.0     // 酸碱度
	SpecificGravMin, SpecificGravMax = 1.005, 1.030 // 比重
)

// SerialFrame OPM-1560B串口原始帧模型（贴合硬件帧格式：AA+数据段+校验位+55）
type SerialFrame struct {
	Start    []byte `json:"start"`     // 帧头（0xAA）
	Data     []byte `json:"data"`      // 核心数据段
	CheckSum byte   `json:"check_sum"` // 校验位（和校验，帧尾前1字节）
	End      []byte `json:"end"`       // 帧尾（0x55）
	Raw      []byte `json:"raw"`       // 原始字节数组（用于调试/回溯）
	Length   int    `json:"length"`    // 帧总长度
}

// OPM1560BDeviceData OPM-1560B核心检测数据模型（贴合设备12项标配检测项，硬件数据段一一映射）
type OPM1560BDeviceData struct {
	DeviceID     string  `json:"device_id"`     // 设备出厂SN
	DeviceModel  string  `json:"device_model"`  // 固定OPM-1560B
	TestTime     string  `json:"test_time"`     // 检测时间（RFC3339，UTC）
	PH           float64 `json:"ph"`            // 酸碱度（BCD码解析后浮点数）
	Protein      string  `json:"protein"`       // 尿蛋白（-/+/±/++/+++/++++）
	Glucose      string  `json:"glucose"`       // 葡萄糖（同尿蛋白编码）
	Ketone       string  `json:"ketone"`        // 酮体（同尿蛋白编码）
	OccultBlood  string  `json:"occult_blood"`  // 隐血（同尿蛋白编码）
	Leukocyte    string  `json:"leukocyte"`     // 白细胞（同尿蛋白编码）
	Erythrocyte  string  `json:"erythrocyte"`   // 红细胞（同尿蛋白编码）
	Urobilinogen string  `json:"urobilinogen"`  // 尿胆原（同尿蛋白编码）
	Bilirubin    string  `json:"bilirubin"`     // 胆红素（同尿蛋白编码）
	Nitrite      string  `json:"nitrite"`       // 亚硝酸盐（-/+/invalid）
	SpecificGrav float64 `json:"specific_grav"` // 比重（BCD码解析后浮点数）
	VC           string  `json:"vc"`            // 维生素C（同尿蛋白编码）
	DataState    string  `json:"data_state"`    // 数据状态：normal/abnormal/invalid
	RawFrameHex  string  `json:"raw_frame_hex"` // 原始帧16进制字符串（调试/溯源）
}

// MQTTMessage 标准化MQTT上报模型（物联网平台通用格式，避免平台适配成本）
type MQTTMessage struct {
	DeviceID    string      `json:"device_id"`    // 设备SN
	DeviceModel string      `json:"device_model"` // OPM-1560B
	MsgType     string      `json:"msg_type"`     // data/state
	Content     interface{} `json:"content"`      // 检测数据/设备状态
	ReportTime  string      `json:"report_time"`  // 上报时间（RFC3339，UTC）
	Version     string      `json:"version"`      // 消息版本，固定v1.0
}

// NewSerialFrame 新建串口原始帧实例（封装帧解析逻辑，避免重复代码）
func NewSerialFrame(raw []byte, start, end []byte, checkSum byte) *SerialFrame {
	return &SerialFrame{
		Start:    start,
		Data:     raw[len(start) : len(raw)-len(end)-1], // 数据段：帧头后 → 校验位前
		CheckSum: checkSum,
		End:      end,
		Raw:      raw,
		Length:   len(raw),
	}
}

// NewOPM1560BDeviceData 新建检测数据实例（初始化基础字段，避免重复赋值）
func NewOPM1560BDeviceData(deviceID, model string) *OPM1560BDeviceData {
	return &OPM1560BDeviceData{
		DeviceID:    deviceID,
		DeviceModel: model,
		TestTime:    time.Now().UTC().Format(time.RFC3339),
		DataState:   DataStateNormal, // 默认正常，后续校验修正
	}
}

// CheckDataValid 校验检测数据医学有效性（核心：标记abnormal状态，贴合医用需求）
func (d *OPM1560BDeviceData) CheckDataValid() {
	// PH值超出合理范围
	if d.PH < PHMin || d.PH > PHMax {
		d.DataState = DataStateAbnormal
	}
	// 比重超出合理范围
	if d.SpecificGrav < SpecificGravMin || d.SpecificGrav > SpecificGravMax {
		d.DataState = DataStateAbnormal
	}
}

// NewMQTTMessage 新建标准化MQTT消息实例（封装通用字段，统一上报格式）
func NewMQTTMessage(deviceID, model, msgType string, content interface{}) *MQTTMessage {
	return &MQTTMessage{
		DeviceID:    deviceID,
		DeviceModel: model,
		MsgType:     msgType,
		Content:     content,
		ReportTime:  time.Now().UTC().Format(time.RFC3339),
		Version:     "v1.0",
	}
}

// ToJSON MQTT消息转JSON字节数组（MQTT发布专用，处理序列化错误）
func (m *MQTTMessage) ToJSON() ([]byte, error) {
	return json.Marshal(m)
}

// HexStr 工具方法：字节数组转16进制字符串（日志/调试用）
func HexStr(b []byte) string {
	hex, _ := json.Marshal(b)
	return string(hex[1 : len(hex)-1])
}
