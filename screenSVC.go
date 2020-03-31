package main

import (
	"errors"
	"fmt"
	"golang.org/x/net/websocket"
	"io/ioutil"
	"time"
)

var client_screenConn = NewLockMapSC() //客户端唯一识别码和屏幕查看器conn对应
//客户端屏幕查看器(忽略加密压缩，直接中转传输)
func screenhfuiefdhuiwe32uhi(conn *websocket.Conn) {
	defer func() {
		if conn != nil {
			conn.Close()
		}
	}()
	//验证是否允许连接
	ip, machineid, flag := checkConn(conn)
	if flag == false {
		return
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	fmt.Println(now, ip, machineid, "屏幕查看器连接成功:")
	client_screenConn.Set(machineid, conn)    //存对应关系
	defer client_screenConn.Delete(machineid) //删除对应关系
	for {
		//先查看客户端服务器是否连接，断开连接不提供视频
		if _, ok := clients.exist(machineid); !ok {
			fmt.Println(now, ip, machineid, "客户端主程序断开连接，屏幕查看器即将退出")
			return
		}
		reqbyts, err := screenReadMessage(conn) //接客户端收传回来的消息传给控制台
		now := time.Now().Format("2006-01-02 15:04:05")
		if err != nil {
			fmt.Println(now, ip, machineid, "屏幕查看器断开连接", err)
			return
		}
		if controlUuid, ok := clients_control.exist(machineid); ok { //查看此客户端唯一id对应的控制台唯一id是否存在
			if conn, ok2 := control.exist(controlUuid); ok2 { //拿到此控制台的连接
				_ = screenSendMessage(reqbyts, conn)
			} else {
				fmt.Println(now, ip, machineid, "控制台连接池不存在此连接,屏幕查看器即将退出")
				return
			}
		} else {
			fmt.Println(now, ip, machineid, "控制台未操作此客户端,屏幕查看器即将退出")
			return
		}
	}
}

//读取数据
func screenReadMessage(conn *websocket.Conn) ([]byte, error) {
again:
	fr, err := conn.NewFrameReader()

	if err != nil {
		return nil, err
	}
	frame, err := conn.HandleFrame(fr)
	if err != nil {
		return nil, err
	}
	if frame == nil {
		goto again
	}
	reqBytes, err := ioutil.ReadAll(frame)
	if err != nil {
		return reqBytes, err
	}
	return reqBytes, nil
}

//发送websocket消息
func screenSendMessage(jsonBytes []byte, conn *websocket.Conn) error {
	if conn != nil {
		_, err := conn.Write(jsonBytes) //发送消息
		return err
	} else {
		return errors.New("conn is null pointer")
	}

}

//关闭屏幕查看器
func offScreen(message Message) {
	machineidA := control_clients.Get(message.Uuid)
	conn := client_screenConn.Get(machineidA)
	sendMessage(message, conn)
	client_screenConn.Delete(machineidA) //删除对应关系
	if conn != nil {
		conn.Close()
	}
}
