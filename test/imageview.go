// Copyright 2017 The Walk Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"github.com/kbinani/screenshot"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

var screenMW *walk.MainWindow
var screen *walk.ImageView
var bitmap *walk.Bitmap

func main() {
	defer fmt.Println(111)
	_, _ = MainWindow{
		OnKeyDown: func(key walk.Key) {
			if bitmap != nil {
				bitmap.Dispose()
			}
			img, _ := screenshot.CaptureDisplay(0)
			bitmap, _ := walk.NewBitmapFromImage(img)
			screen.SetImage(bitmap)
		},
		AssignTo: &screenMW,
		Title:    "屏幕监控",
		Size:     Size{400, 600},
		Layout:   Grid{Columns: 2},
		Children: []Widget{
			ImageView{
				AssignTo: &screen,
				//Image:    "C:\\Users\\asus\\Desktop\\2.jpg",
				Mode: ImageViewModeStretch,
			},
		},
	}.Run()

}
