package main

import (
	"log"
	"opm-mqtt-gateway/internal/config"
	"opm-mqtt-gateway/internal/models"
	"opm-mqtt-gateway/internal/mqtt"
	"opm-mqtt-gateway/internal/parser"
	"opm-mqtt-gateway/internal/serial"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

// initLog 初始化日志（分级+文件输出，生产级必备，贴合配置）
func initLog(cfg *config.Config) {
	// 创建日志目录（不存在则自动创建）
	logDir := filepath.Dir(cfg.Log.Path)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Fatalf("[FATAL] 创建日志目录失败：%v", err)
	}

	// 打开日志文件（追加模式，保留历史日志）
	logFile, err := os.OpenFile(cfg.Log.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("[FATAL] 打开日志文件失败：%v", err)
	}

	// 配置日志：时间+级别+文件+标准输出双写
	log.SetOutput(logFile)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}

func main() {
	// 1. 加载配置文件（核心：硬件参数校验+默认值）
	configPath := "configs/config.yaml"
	if err := config.Load(configPath); err != nil {
		log.Fatalf("[FATAL] 加载配置失败：%v", err)
	}
	cfg := config.GlobalConfig

	// 2. 初始化日志（生产级分级日志）
	initLog(cfg)

	// 3. 初始化有效帧通道（缓冲区100，适配设备检测频率）
	frameChan := make(chan []byte, 100)

	// 4. 初始化核心模块（串口/MQTT/解析器，贴合硬件特性）
	serialReader, err := serial.NewReader(frameChan)
	if err != nil {
		log.Fatalf("[FATAL] 初始化串口失败：%v", err)
	}
	mqttClient, err := mqtt.NewClient()
	if err != nil {
		log.Fatalf("[FATAL] 初始化MQTT失败：%v", err)
	}
	opmParser := parser.NewParser()

	// 5. 启动串口阅读器（数据采集+粘包拆包+重连）
	serialReader.Start()
	log.Printf("[INFO] [main] 串口阅读器已启动，设备：%s", cfg.Device.DeviceID)

	// 6. 启动数据处理协程（核心链路：串口帧→解析→MQTT发布）
	go func() {
		for frame := range frameChan {
			// 容错1：MQTT未连接，丢弃帧并记录日志
			if !mqttClient.IsConnected() {
				log.Printf("[WARN] [main] MQTT未连接，丢弃帧：%s", models.HexStr(frame))
				continue
			}

			// 解析串口帧为检测数据
			deviceData, err := opmParser.Parse(frame)
			if err != nil {
				log.Printf("[ERROR] [main] 解析帧失败：%v，帧：%s", err, models.HexStr(frame))
				continue
			}

			// 构建标准化MQTT消息
			mqttMsg := models.NewMQTTMessage(
				cfg.Device.DeviceID,
				cfg.Device.Model,
				models.MQTTMsgTypeData,
				deviceData,
			)

			// 发布MQTT消息（医用数据QoS1，保证至少送达）
			if err := mqttClient.Publish(mqttMsg); err != nil {
				log.Printf("[ERROR] [main] 发布MQTT失败：%v，数据：%+v", err, deviceData)
				continue
			}

			log.Printf("[INFO] [main] 数据处理完成，设备：%s，检测时间：%s，状态：%s",
				deviceData.DeviceID, deviceData.TestTime, deviceData.DataState)
		}
	}()
	log.Printf("[INFO] [main] 数据处理协程已启动，全链路就绪")

	// 7. 捕获系统退出信号（SIGINT/SIGTERM），实现优雅退出（生产级必备）
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan // 阻塞等待退出信号

	// 8. 优雅关闭所有模块（按顺序：串口→MQTT，释放所有资源）
	log.Printf("[INFO] [main] 接收到退出信号，开始优雅关闭...")
	serialReader.Close()
	mqttClient.Close()
	log.Printf("[INFO] [main] 所有模块已关闭，程序正常退出")
}
