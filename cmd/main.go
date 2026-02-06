package main

import (
	"log"
	"opm-mqtt-gateway/internal/config"
	"opm-mqtt-gateway/internal/mqtt"
	"opm-mqtt-gateway/internal/parser"
	"opm-mqtt-gateway/internal/serial"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// 1.加载配置
	cfg, err := config.LoadConfig("configs/config.yaml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 2.初始化日志
	if err := config.InitLogging(&cfg.Logging); err != nil {
		log.Fatalf("初始化日志失败: %v", err)
	}

	log.Printf("启动OPM-1560B数据读取器 v%s", cfg.App.Version)

	// 3.初始化串口读取器
	serialReader := serial.NewSerialReader(&cfg.Serial)

	// 4.尝试打开串口
	var serialErr error
	for i := 0; i < 3; i++ {
		serialErr = serialReader.Open()
		if serialErr == nil {
			break
		}
		log.Printf("串口打开失败(尝试 %d/3): %v", i+1, serialErr)
		if i < 2 {
			time.Sleep(2 * time.Second)
		}
	}

	if serialErr != nil {
		log.Fatalf("无法打开串口: %v", serialErr)
	}
	defer serialReader.Close()

	// 5.初始化MQTT客户端
	var mqttClient *mqtt.MQTTClient
	if cfg.MQTT.Broker != "" {
		mqttClient = mqtt.NewMQTTClient(&cfg.MQTT)
		if err := mqttClient.Connect(); err != nil {
			log.Printf("MQTT连接失败: %v (继续运行，仅记录数据)", err)
		} else {
			defer mqttClient.Disconnect()
			log.Printf("MQTT连接成功")
		}
	} else {
		log.Printf("未配置有效MQTT Broker，跳过MQTT连接")
	}

	// 6.初始化数据解析器
	dataParser := parser.NewParser()

	if err := serialReader.StartReading(); err != nil {
		log.Fatalf("启动串口读取失败: %v", err)
	}

	log.Println("数据读取服务已启动，等待设备数据...")

	// 7.信号处理
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	dataChan := serialReader.GetDataChan()

	for {
		select {
		case data := <-dataChan:
			if len(data) > 0 {
				log.Printf("📨 收到原始数据: %d 字节", len(data))

				// 显示数据内容
				displayLen := min(100, len(data))
				safeData := make([]byte, displayLen)
				copy(safeData, data[:displayLen])
				log.Printf("数据内容(前%d字符): %q", displayLen, string(safeData))

				result, err := dataParser.ParseData(data)
				if err != nil {
					log.Printf("❌ 数据解析失败: %v", err)
					continue
				}

				if result != nil {
					log.Printf("✅ 解析到有效数据: 样本号=%s, 日期=%s, 时间=%s, 项目数=%d",
						result.SampleID, result.TestDate.Format("2006-01-02"),
						result.TestTime, len(result.Items))

					// 打印详细结果
					for i, item := range result.Items {
						log.Printf("  %2d. %-8s: %s", i+1, item.Name, item.Value)
					}

					// 发送到MQTT
					if mqttClient != nil && mqttClient.IsConnected() {
						if err := mqttClient.PublishResult(result); err != nil {
							log.Printf("❌ MQTT发布失败: %v", err)
						} else {
							log.Printf("📤 MQTT发布成功: topic=%s", cfg.MQTT.Topic)
						}
					} else {
						log.Printf("ℹ️  MQTT未连接，数据仅记录到日志")
					}
				} else {
					log.Printf("⏳ 数据不完整，等待更多数据...")
				}
			}

		case sig := <-signalChan:
			log.Printf("接收到信号: %v，正在关闭...", sig)
			return

		case <-time.After(60 * time.Second):
			// 定期心跳
			log.Printf("服务运行中...")
		}
	}
}
