package main

import (
	"fmt"
	"log"
	"time"

	"go.bug.st/serial"
)

func main() {
	mode := &serial.Mode{
		BaudRate: 9600,
		DataBits: 8,
		StopBits: serial.OneStopBit,
		Parity:   serial.OddParity,
	}

	port, err := serial.Open("COM2", mode)
	if err != nil {
		log.Fatalf("打开测试串口失败: %v", err)
	}
	defer port.Close()

	// 生成符合OPM-1560B格式的测试数据
	//testData := []string{
	//	"2026-02-03\r\n10:15:30\r\n001\r\n\r\nGLU\tNegative\r\nBIL\tNegative\r\nSG\t1.015\r\nPH\t6.0\r\nKET\tNegative\r\nBLD\tNegative\r\nPRO\tNegative\r\nURO\tNormal\r\nNIT\tNegative\r\nLEU\tNegative\r\n",
	//	"2026-02-03\r\n10:20:05\r\n002\r\n\r\nGLU\t*250 mg/dL\r\nBIL\t*+\r\nSG\t1.030\r\nPH\t*8.5\r\nKET\t*+++\r\nBLD\t*+\r\nPRO\t*150 mg/dL\r\nURO\tNormal\r\nNIT\tNegative\r\nLEU\t*++\r\n",
	//	"2026-02-03\r\n10:25:40\r\n003\r\n\r\nGLU\tNormal\r\nBIL\tNegative\r\nSG\t*1.005\r\nPH\t5.0\r\nKET\tNegative\r\nBLD\tTrace\r\nPRO\t*80 mg/dL\r\nURO\t*4 mg/dL\r\nNIT\tNegative\r\nLEU\t+\r\n",
	//}
	testData := []string{generateTestData()}

	for _, data := range testData {
		// 发送测试数据
		n, err := port.Write([]byte(data))
		if err != nil {
			log.Fatalf("发送测试数据失败: %v", err)
		}

		fmt.Printf("测试数据发送成功: %d 字节\n", n)
		fmt.Printf("数据内容:\n%s\n", testData)
	}
}

func generateTestData() string {
	now := time.Now()
	dateStr := now.Format("2006-01-02")
	timeStr := now.Format("15:04:05")

	return fmt.Sprintf("%s\r\n%s\r\n001\r\n\r\n"+
		"GLU\t阴性\r\n"+
		"BIL\t-\r\n"+
		"SG\t1.015\r\n"+
		"PH\t6.0\r\n"+
		"KET\t阴性\r\n"+
		"BLD\t阴性\r\n"+
		"PRO\t阴性\r\n"+
		"URO\t正常\r\n"+
		"NIT\t阴性\r\n"+
		"LEU\t阴性\r\n",
		dateStr, timeStr)
}
