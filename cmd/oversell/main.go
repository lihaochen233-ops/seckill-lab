// 故意错误的对照实验，仅此命令使用，HTTP 服务不会调用它。
package main

import (
	"fmt"
	"sync"
)

func main() {
	stock, orders := 1, 0
	var mu sync.Mutex
	var readDone, done sync.WaitGroup
	readDone.Add(2)
	done.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer done.Done()
			mu.Lock()
			snapshot := stock
			mu.Unlock()
			readDone.Done()
			readDone.Wait() // 保证两个用户都先读到 1，再开始写回。
			if snapshot > 0 {
				mu.Lock()
				stock = snapshot - 1
				orders++
				mu.Unlock()
			}
		}()
	}
	done.Wait()
	fmt.Printf("错误示范：初始库存=1，剩余库存=%d，订单数=%d\n", stock, orders)
	fmt.Println("单次读写都有锁，但整个下单过程不原子，所以依然超卖。")
}
