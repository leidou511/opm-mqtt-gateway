package main

import (
	"fmt"
	"log"

	"go.bug.st/serial"
)

func main() {
	fmt.Println("=== OPM-1560B 串口检测工具 ===")

	// 获取可用串口
	ports, err := serial.GetPortsList()
	if err != nil {
		log.Fatalf("获取串口列表失败: %v", err)
	}

	if len(ports) == 0 {
		fmt.Println("未找到任何串口设备")
		return
	}

	fmt.Printf("找到 %d 个串口设备:\n", len(ports))
	for i, port := range ports {
		fmt.Printf("%d. %s\n", i+1, port)
	}

	// 测试OPM-1560B推荐配置
	fmt.Println("\n=== 测试OPM-1560B推荐配置 ===")

	mode := &serial.Mode{
		BaudRate: 9600,
		DataBits: 8,
		StopBits: serial.OneStopBit,
		Parity:   serial.OddParity,
	}

	for _, port := range ports {
		fmt.Printf("测试串口 %s: ", port)

		p, err := serial.Open(port, mode)
		if err != nil {
			fmt.Printf("❌ 失败 - %v\n", err)
			continue
		}

		fmt.Printf("✅ 成功\n")
		p.Close()
	}
}
