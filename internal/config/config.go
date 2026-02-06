package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// 全局配置实例，供所有模块调用
var GlobalConfig *Config

// Config 项目总配置，包含OPM-1560B专属/串口/MQTT/解析/日志配置
type Config struct {
	Device DeviceConfig `yaml:"device" comment:"OPM-1560B设备专属配置（必填SN）"`
	Serial SerialConfig `yaml:"serial" comment:"串口配置（硬件固化参数默认）"`
	MQTT   MQTTConfig   `yaml:"mqtt"   comment:"MQTT配置（医用数据QoS1默认）"`
	Log    LogConfig    `yaml:"log"    comment:"日志配置"`
	Parser ParserConfig `yaml:"parser" comment:"协议解析配置（硬件帧格式固定）"`
}

// DeviceConfig OPM-1560B设备专属配置
type DeviceConfig struct {
	DeviceID string `yaml:"device_id" comment:"设备唯一SN编号（必填，出厂固化）"`
	Model    string `yaml:"model"    comment:"设备型号，固定为OPM-1560B"`
}

// SerialConfig 串口配置（OPM-1560B硬件固化：9600/8/1/none，不可修改）
type SerialConfig struct {
	Port     string `yaml:"port"       comment:"串口名：Linux-/dev/ttyUSBx，Windows-COMx"`
	BaudRate int    `yaml:"baud_rate"  comment:"波特率，仅支持9600/19200（硬件约束）"`
	DataBits int    `yaml:"data_bits"  comment:"数据位，固定8（硬件约束，不可改）"`
	StopBits int    `yaml:"stop_bits"  comment:"停止位，固定1（硬件约束，不可改）"`
	Parity   string `yaml:"parity"     comment:"校验位，固定none（硬件约束，不可改）"`
	Timeout  int    `yaml:"timeout"    comment:"串口读写超时，单位秒，默认3"`
	RetryCnt int    `yaml:"retry_cnt"  comment:"串口打开重试次数，默认3"`
	RetryInt int    `yaml:"retry_int"  comment:"串口重试间隔，单位秒，默认2"`
}

// MQTTConfig MQTT配置（医用数据推荐QoS1，保证至少送达）
type MQTTConfig struct {
	Broker       string `yaml:"broker"        comment:"MQTT服务端：tcp://ip:port"`
	ClientID     string `yaml:"client_id"     comment:"客户端ID，为空则使用device_id"`
	Username     string `yaml:"username"      comment:"MQTT用户名，无则留空"`
	Password     string `yaml:"password"      comment:"MQTT密码，无则留空"`
	TopicPrefix  string `yaml:"topic_prefix"  comment:"主题前缀，最终：前缀/device_id/data"`
	QoS          int    `yaml:"qos"           comment:"QoS级别，推荐1（医用数据不丢失）"`
	KeepAlive    int    `yaml:"keep_alive"    comment:"保活时间，单位秒，默认30"`
	ReconnectInt int    `yaml:"reconnect_int" comment:"重连基础间隔，单位秒，默认2"`
	WillTopic    string `yaml:"will_topic"    comment:"遗嘱主题，为空则自动生成"`
	WillMsg      string `yaml:"will_msg"      comment:"遗嘱消息，离线时发送offline"`
	WillQoS      int    `yaml:"will_qos"      comment:"遗嘱QoS，默认1"`
	WillRetain   bool   `yaml:"will_retain"   comment:"遗嘱是否保留，默认true"`
}

// LogConfig 日志配置
type LogConfig struct {
	Path  string `yaml:"path"  comment:"日志文件路径，默认logs/app.log"`
	Level string `yaml:"level" comment:"日志级别：INFO/WARN/ERROR/FATAL，默认INFO"`
}

// ParserConfig 协议解析配置（OPM-1560B硬件固定：AA帧头/55帧尾/和校验）
type ParserConfig struct {
	FrameStart  string `yaml:"frame_start"  comment:"帧头，16进制，固定AA（硬件约束）"`
	FrameEnd    string `yaml:"frame_end"    comment:"帧尾，16进制，固定55（硬件约束）"`
	CheckType   string `yaml:"check_type"   comment:"校验方式，固定sum（和校验，硬件约束）"`
	FrameMinLen int    `yaml:"frame_min_len" comment:"最小帧长度，固定16（硬件约束）"`
}

// Load 加载配置文件，执行：默认值设置→环境变量覆盖→硬件合法性校验
func Load(configPath string) error {
	// 1. 读取YAML配置文件
	file, err := os.Open(configPath)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}
	defer file.Close()

	var cfg Config
	if err := yaml.NewDecoder(file).Decode(&cfg); err != nil {
		return fmt.Errorf("解析YAML失败: %w", err)
	}

	// 2. 设置硬件固化默认值（核心：配置缺失时自动兜底，避免运行失败）
	setHardwareDefaults(&cfg)
	// 3. 环境变量覆盖配置（适配容器化，无需修改配置文件）
	overrideByEnv(&cfg)
	// 4. 硬件强约束校验（非法配置直接终止，杜绝通信失败）
	if err := validateHardwareConfig(&cfg); err != nil {
		return fmt.Errorf("硬件配置校验失败: %w", err)
	}

	// 5. 赋值全局配置
	GlobalConfig = &cfg
	fmt.Printf("[INFO] 配置加载成功，设备SN：%s，串口：%s，MQTT服务端：%s\n", cfg.Device.DeviceID, cfg.Serial.Port, cfg.MQTT.Broker)
	return nil
}

// setHardwareDefaults 为所有配置设置OPM-1560B硬件固化默认值
func setHardwareDefaults(cfg *Config) {
	// 设备默认值
	if cfg.Device.Model == "" {
		cfg.Device.Model = "OPM-1560B"
	}

	// 串口默认值（硬件固化：9600/8/1/none）
	if cfg.Serial.BaudRate == 0 {
		cfg.Serial.BaudRate = 9600
	}
	if cfg.Serial.DataBits == 0 {
		cfg.Serial.DataBits = 8
	}
	if cfg.Serial.StopBits == 0 {
		cfg.Serial.StopBits = 1
	}
	if cfg.Serial.Parity == "" {
		cfg.Serial.Parity = "none"
	}
	if cfg.Serial.Timeout == 0 {
		cfg.Serial.Timeout = 3
	}
	if cfg.Serial.RetryCnt == 0 {
		cfg.Serial.RetryCnt = 3
	}
	if cfg.Serial.RetryInt == 0 {
		cfg.Serial.RetryInt = 2
	}

	// MQTT默认值（医用数据优化：QoS1+遗嘱）
	if cfg.MQTT.TopicPrefix == "" {
		cfg.MQTT.TopicPrefix = "opm1560b/urine/analyzer"
	}
	if cfg.MQTT.QoS == 0 {
		cfg.MQTT.QoS = 1
	}
	if cfg.MQTT.KeepAlive == 0 {
		cfg.MQTT.KeepAlive = 30
	}
	if cfg.MQTT.ReconnectInt == 0 {
		cfg.MQTT.ReconnectInt = 2
	}
	if cfg.MQTT.ClientID == "" {
		cfg.MQTT.ClientID = cfg.Device.DeviceID
	}
	if cfg.MQTT.WillTopic == "" {
		cfg.MQTT.WillTopic = fmt.Sprintf("%s/%s/state", cfg.MQTT.TopicPrefix, cfg.Device.DeviceID)
	}
	if cfg.MQTT.WillMsg == "" {
		cfg.MQTT.WillMsg = "offline"
	}
	if cfg.MQTT.WillQoS == 0 {
		cfg.MQTT.WillQoS = 1
	}
	if !cfg.MQTT.WillRetain {
		cfg.MQTT.WillRetain = true
	}

	// 日志默认值
	if cfg.Log.Path == "" {
		cfg.Log.Path = "logs/app.log"
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "INFO"
	}

	// 解析器默认值（硬件固化：AA/55/和校验/16字节最小帧）
	if cfg.Parser.FrameStart == "" {
		cfg.Parser.FrameStart = "AA"
	}
	if cfg.Parser.FrameEnd == "" {
		cfg.Parser.FrameEnd = "55"
	}
	if cfg.Parser.CheckType == "" {
		cfg.Parser.CheckType = "sum"
	}
	if cfg.Parser.FrameMinLen == 0 {
		cfg.Parser.FrameMinLen = 16
	}
}

// overrideByEnv 环境变量覆盖配置，格式：OPM_模块_字段（如OPM_SERIAL_PORT=/dev/ttyUSB1）
func overrideByEnv(cfg *Config) {
	// 设备配置
	if v := os.Getenv("OPM_DEVICE_DEVICEID"); v != "" {
		cfg.Device.DeviceID = v
	}
	// 串口配置
	if v := os.Getenv("OPM_SERIAL_PORT"); v != "" {
		cfg.Serial.Port = v
	}
	if v := os.Getenv("OPM_SERIAL_BAUDRATE"); v != "" {
		if br, err := strconv.Atoi(v); err == nil {
			cfg.Serial.BaudRate = br
		}
	}
	// MQTT核心配置
	if v := os.Getenv("OPM_MQTT_BROKER"); v != "" {
		cfg.MQTT.Broker = v
	}
	if v := os.Getenv("OPM_MQTT_USERNAME"); v != "" {
		cfg.MQTT.Username = v
	}
	if v := os.Getenv("OPM_MQTT_PASSWORD"); v != "" {
		cfg.MQTT.Password = v
	}
}

// validateHardwareConfig OPM-1560B硬件强约束校验（非法配置直接返回错误）
func validateHardwareConfig(cfg *Config) error {
	// 1. 设备校验：SN编号为必填项（出厂固化，唯一标识）
	if cfg.Device.DeviceID == "" {
		return errors.New("device.device_id 为必填项（请填写设备出厂SN编号）")
	}

	// 2. 串口校验（硬件固化约束，不可突破）
	if cfg.Serial.Port == "" {
		return errors.New("serial.port 为必填项（Linux:/dev/ttyUSBx，Windows:COMx）")
	}
	if cfg.Serial.BaudRate != 9600 && cfg.Serial.BaudRate != 19200 {
		return errors.New("serial.baud_rate 仅支持9600/19200（OPM-1560B硬件固化）")
	}
	if cfg.Serial.DataBits != 8 {
		return errors.New("serial.data_bits 必须为8（OPM-1560B硬件固化，不可修改）")
	}
	if cfg.Serial.StopBits != 1 {
		return errors.New("serial.stop_bits 必须为1（OPM-1560B硬件固化，不可修改）")
	}

	// 3. MQTT校验
	if cfg.MQTT.Broker == "" {
		return errors.New("mqtt.broker 为必填项（格式：tcp://ip:port）")
	}
	if cfg.MQTT.QoS < 0 || cfg.MQTT.QoS > 2 {
		return errors.New("mqtt.qos 仅支持0/1/2（推荐1，医用数据不丢失）")
	}

	// 4. 解析器校验（硬件帧格式约束）
	if _, err := hexStrToBytes(cfg.Parser.FrameStart); err != nil {
		return fmt.Errorf("parser.frame_start 非法16进制：%w", err)
	}
	if _, err := hexStrToBytes(cfg.Parser.FrameEnd); err != nil {
		return fmt.Errorf("parser.frame_end 非法16进制：%w", err)
	}
	if cfg.Parser.CheckType != "sum" {
		return errors.New("parser.check_type 仅支持sum（和校验，OPM-1560B硬件固化）")
	}
	if cfg.Parser.FrameMinLen < 16 {
		return errors.New("parser.frame_min_len 最小16字节（OPM-1560B硬件帧格式）")
	}

	// 5. 日志级别校验
	validLevels := map[string]bool{"INFO": true, "WARN": true, "ERROR": true, "FATAL": true}
	if !validLevels[cfg.Log.Level] {
		return errors.New("log.level 仅支持INFO/WARN/ERROR/FATAL")
	}

	return nil
}

// 工具方法：16进制字符串转字节数组（帧头/帧尾解析）
func hexStrToBytes(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if len(s)%2 != 0 {
		return nil, errors.New("长度为奇数")
	}
	b := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		if _, err := fmt.Sscanf(s[i:i+2], "%02x", &b[i/2]); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// GetFrameStart 全局快捷方法：获取帧头字节数组（避免各模块重复解析）
func GetFrameStart() []byte {
	b, _ := hexStrToBytes(GlobalConfig.Parser.FrameStart)
	return b
}

// GetFrameEnd 全局快捷方法：获取帧尾字节数组
func GetFrameEnd() []byte {
	b, _ := hexStrToBytes(GlobalConfig.Parser.FrameEnd)
	return b
}
