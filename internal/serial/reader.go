package serial

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"opm-mqtt-gateway/internal/config"
	"opm-mqtt-gateway/internal/models"

	"go.bug.st/serial"
)

// Reader OPM-1560B串口阅读器实例（贴合硬件串口特性，基于serial v1.6.4实现）
type Reader struct {
	port        serial.Port        // 串口端口句柄
	portMode    serial.Mode        // 串口配置（映射硬件参数）
	portName    string             // 串口号
	ctx         context.Context    // 协程管理上下文
	cancel      context.CancelFunc // 协程取消函数
	mu          sync.Mutex         // 读写互斥锁（并发安全）
	buffer      []byte             // 数据缓冲区（处理粘包/拆包）
	frameChan   chan []byte        // 有效帧输出通道（传给解析器）
	isConnected bool               // 串口连接状态
	retryCnt    int                // 打开重试次数
	retryInt    time.Duration      // 重试间隔
	readTimeout time.Duration      // 读超时（防止协程阻塞）
}

// NewReader 新建串口阅读器实例（基于全局硬件配置初始化，带重试）
func NewReader(frameChan chan []byte) (*Reader, error) {
	cfg := config.GlobalConfig
	// 1. 映射硬件串口参数到serial.Mode（贴合OPM-1560B固化特性）
	portMode := serial.Mode{
		BaudRate: cfg.Serial.BaudRate,
		DataBits: cfg.Serial.DataBits,
		StopBits: serial.OneStopBit,
	}

	switch cfg.Serial.Parity {
	case "O", "ODD":
		portMode.Parity = serial.OddParity
	case "E", "EVEN":
		portMode.Parity = serial.EvenParity
	case "N", "NONE":
		portMode.Parity = serial.NoParity
	default:
		portMode.Parity = serial.OddParity // 默认奇校验
	}

	log.Printf("串口配置: 波特率=%d, 数据位=%d, 停止位=%d, 校验位=%v", portMode.BaudRate, portMode.DataBits, portMode.StopBits, portMode.Parity)

	// 2. 初始化上下文
	ctx, cancel := context.WithCancel(context.Background())

	// 3. 新建实例
	r := &Reader{
		portMode:    portMode,
		portName:    cfg.Serial.Port,
		ctx:         ctx,
		cancel:      cancel,
		frameChan:   frameChan,
		buffer:      make([]byte, 0, 1024), // 缓冲区初始容量1024，适配设备帧长度
		retryCnt:    cfg.Serial.RetryCnt,
		retryInt:    time.Duration(cfg.Serial.RetryInt) * time.Second,
		readTimeout: time.Duration(cfg.Serial.Timeout) * time.Second,
		isConnected: false,
	}

	// 4. 打开串口（带重试，解决工业现场端口偶发占用）
	if err := r.openWithRetry(); err != nil {
		return nil, fmt.Errorf("串口打开失败: %w", err)
	}

	log.Printf("[INFO] [serial] 串口初始化成功，设备：%s，波特率：%d", r.portName, cfg.Serial.BaudRate)
	return r, nil
}

// Start 启动串口核心协程：数据读取+粘包拆包+断线重连（7*24运行）
func (r *Reader) Start() {
	go func() {
		for {
			select {
			case <-r.ctx.Done():
				// 上下文取消，优雅关闭
				r.Close()
				log.Printf("[INFO] [serial] 串口协程正常退出")
				return
			default:
				if !r.isConnected {
					// 串口断开，自动重连
					log.Printf("[WARN] [serial] 串口断开，开始重连（间隔：%v）", r.retryInt)
					if err := r.openWithRetry(); err != nil {
						time.Sleep(r.retryInt)
						continue
					}
					log.Printf("[INFO] [serial] 串口重连成功：%s", r.portName)
				}

				// 读取串口数据（带超时）
				data, err := r.readData()
				if err != nil {
					log.Printf("[ERROR] [serial] 读数据失败：%v，标记断开", err)
					r.mu.Lock()
					r.isConnected = false
					r.mu.Unlock()
					_ = r.port.Close() // 释放句柄，防止泄漏
					time.Sleep(r.retryInt)
					continue
				}

				// 处理数据，提取有效帧（核心：解决粘包/拆包）
				if len(data) > 0 {
					r.handleData(data)
				}
			}
		}
	}()
}

// openWithRetry 串口打开（带重试机制，工业现场必备）
func (r *Reader) openWithRetry() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var err error
	for i := 1; i <= r.retryCnt; i++ {
		// 先检查串口是否存在（减少无效重试）
		if !r.isPortExist() {
			err = fmt.Errorf("串口%s不存在", r.portName)
			log.Printf("[ERROR] [serial] 重试%d/%d：%v", i, r.retryCnt, err)
			time.Sleep(r.retryInt)
			continue
		}

		// 打开串口（serial v1.6.4标准方法）
		port, err := serial.Open(r.portName, &r.portMode)
		if err != nil {
			log.Printf("[ERROR] [serial] 重试%d/%d：打开失败：%v", i, r.retryCnt, err)
			time.Sleep(r.retryInt)
			continue
		}

		// 打开成功，初始化参数
		r.port = port
		r.isConnected = true
		return nil
	}
	return fmt.Errorf("重试%d次后失败：%v", r.retryCnt, err)
}

// isPortExist 检查串口是否存在（辅助工具，排查硬件连接问题）
func (r *Reader) isPortExist() bool {
	ports, err := serial.GetPortsList()
	if err != nil {
		log.Printf("[WARN] [serial] 枚举串口失败，跳过存在性检查：%v", err)
		return true
	}
	for _, p := range ports {
		if p == r.portName {
			return true
		}
	}
	return false
}

// readData 读取串口数据（带超时，防止协程阻塞，serial v1.6.4标准方法）
func (r *Reader) readData() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.port == nil {
		return nil, errors.New("端口句柄未初始化")
	}

	// 设置读超时
	if err := r.port.SetReadTimeout(r.readTimeout); err != nil {
		return nil, fmt.Errorf("设置超时失败：%w", err)
	}

	// 读取数据（缓冲区128字节，适配OPM-1560B单帧最大长度）
	buf := make([]byte, 128)
	n, err := r.port.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("读操作失败：%w", err)
	}

	return buf[:n], nil
}

// handleData 核心：处理串口数据，提取OPM-1560B有效帧（解决粘包/拆包）
// 硬件帧规则：AA开头 → 数据段 → 校验位 → 55结尾，基于帧头帧尾做缓冲区裁剪
func (r *Reader) handleData(data []byte) {
	r.mu.Lock()
	r.buffer = append(r.buffer, data...) // 新数据拼接到缓冲区
	bufLen := len(r.buffer)
	r.mu.Unlock()

	// 硬件帧配置
	frameStart := config.GetFrameStart()
	frameEnd := config.GetFrameEnd()
	minFrameLen := config.GlobalConfig.Parser.FrameMinLen
	startLen, endLen := len(frameStart), len(frameEnd)

	// 缓冲区数据不足最小帧长度，直接返回
	if bufLen < minFrameLen {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// 循环提取有效帧（处理粘包：多帧拼接；处理拆包：单帧拆分）
	for {
		bufLen = len(r.buffer)
		if bufLen < minFrameLen {
			break
		}

		// 1. 查找帧头（AA）位置，无帧头则清空缓冲区
		startIdx := -1
		for i := 0; i <= bufLen-startLen; i++ {
			if compareBytes(r.buffer[i:i+startLen], frameStart) {
				startIdx = i
				break
			}
		}
		if startIdx == -1 {
			log.Printf("[WARN] [serial] 无有效帧头，清空缓冲区")
			r.buffer = make([]byte, 0, 1024)
			break
		}

		// 2. 帧头后数据不足，保留帧头开始的缓冲区（拆包场景）
		if bufLen-startIdx < minFrameLen {
			r.buffer = r.buffer[startIdx:]
			break
		}

		// 3. 查找帧尾（55）位置，无帧尾则保留帧头缓冲区（拆包场景）
		endIdx := -1
		for i := startIdx + minFrameLen - endLen; i <= bufLen-endLen; i++ {
			if compareBytes(r.buffer[i:i+endLen], frameEnd) {
				endIdx = i + endLen // 帧尾结束位置
				break
			}
		}
		if endIdx == -1 {
			r.buffer = r.buffer[startIdx:]
			break
		}

		// 4. 提取有效帧，发送到解析通道
		validFrame := r.buffer[startIdx:endIdx]
		r.frameChan <- validFrame
		log.Printf("[INFO] [serial] 提取有效帧，长度：%d，原始16进制：%s",
			len(validFrame), models.HexStr(validFrame))

		// 5. 裁剪缓冲区：保留帧尾后的数据（粘包场景，下一次循环处理）
		r.buffer = r.buffer[endIdx:]
	}
}

// compareBytes 工具方法：比较两个字节数组是否相等（帧头/帧尾匹配）
func compareBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Close 优雅关闭串口：释放句柄+取消协程+关闭通道（程序退出/重连必备）
func (r *Reader) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.port != nil {
		_ = r.port.Close()
		r.port = nil
		log.Printf("[INFO] [serial] 串口已关闭：%s", r.portName)
	}
	r.isConnected = false
	r.cancel()
	// 通道非空时关闭（防止下游阻塞）
	select {
	case <-r.frameChan:
	default:
		close(r.frameChan)
	}
}

// IsConnected 获取串口连接状态（供上游判断是否可读取数据）
func (r *Reader) IsConnected() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.isConnected
}
