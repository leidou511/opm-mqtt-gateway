package serial

import (
	"fmt"
	"log"
	"opm-mqtt-gateway/internal/config"
	"time"

	"go.bug.st/serial"
)

type SerialReader struct {
	port     serial.Port
	config   *config.SerialConfig
	dataChan chan []byte
	running  bool
}

func NewSerialReader(cfg *config.SerialConfig) *SerialReader {
	return &SerialReader{
		config:   cfg,
		dataChan: make(chan []byte, 1024),
		running:  false,
	}
}

// GetAvailablePorts 获取可用串口列表
func (sr *SerialReader) GetAvailablePorts() ([]string, error) {
	return serial.GetPortsList()
}

func (sr *SerialReader) Open() error {
	log.Printf("正在打开串口: %s", sr.config.Port)

	// 检测可用串口
	ports, err := sr.GetAvailablePorts()
	if err != nil {
		return fmt.Errorf("获取串口列表失败: %v", err)
	}

	if len(ports) == 0 {
		return fmt.Errorf("未找到可用串口")
	}

	log.Printf("可用串口: %v", ports)

	// 创建串口模式配置
	mode := &serial.Mode{
		BaudRate: sr.config.BaudRate,
		DataBits: sr.config.DataBits,
	}

	// 修正停止位映射
	mode.StopBits = serial.OneStopBit // 固定为1个停止位

	// 设置校验位
	switch sr.config.Parity {
	case "O", "ODD":
		mode.Parity = serial.OddParity
	case "E", "EVEN":
		mode.Parity = serial.EvenParity
	case "N", "NONE":
		mode.Parity = serial.NoParity
	default:
		mode.Parity = serial.OddParity // 默认奇校验
	}

	log.Printf("串口配置: 波特率=%d, 数据位=%d, 停止位=%d, 校验位=%v",
		mode.BaudRate, mode.DataBits, mode.StopBits, mode.Parity)

	// 尝试打开串口
	port, err := serial.Open(sr.config.Port, mode)
	if err != nil {
		return fmt.Errorf("打开串口失败: %v (端口=%s)", err, sr.config.Port)
	}

	sr.port = port

	// 重置缓冲区
	if err := sr.port.ResetInputBuffer(); err != nil {
		log.Printf("警告: 重置输入缓冲区失败: %v", err)
	}

	log.Printf("✅ 串口打开成功: %s", sr.config.Port)
	return nil
}

func (sr *SerialReader) StartReading() error {
	if sr.port == nil {
		return fmt.Errorf("串口未打开")
	}

	sr.running = true
	go sr.readLoop()

	log.Printf("串口读取已启动")
	return nil
}

func (sr *SerialReader) readLoop() {
	buffer := make([]byte, 1024)

	for sr.running {
		n, err := sr.port.Read(buffer)
		if err != nil {
			if sr.running {
				log.Printf("读取串口数据错误: %v", err)
			}
			time.Sleep(1 * time.Second)
			continue
		}

		if n > 0 {
			data := make([]byte, n)
			copy(data, buffer[:n])
			log.Printf("接收到 %d 字节数据", n)
			sr.dataChan <- data
		}

		time.Sleep(1 * time.Second)
	}
}

func (sr *SerialReader) GetDataChan() <-chan []byte {
	return sr.dataChan
}

func (sr *SerialReader) Close() error {
	sr.running = false
	if sr.port != nil {
		return sr.port.Close()
	}
	return nil
}
