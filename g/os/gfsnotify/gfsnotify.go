// Copyright 2018 gf Author(https://gitee.com/johng/gf). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://gitee.com/johng/gf.

// 文件监控.
// 使用时需要注意的是，一旦一个文件被删除，那么对其的监控将会失效；如果删除的是目录，那么该目录及其下的文件都将被递归删除监控。
package gfsnotify

import (
    "errors"
    "gitee.com/johng/gf/g/os/glog"
    "gitee.com/johng/gf/third/github.com/fsnotify/fsnotify"
    "gitee.com/johng/gf/g/os/gfile"
    "gitee.com/johng/gf/g/container/gmap"
    "gitee.com/johng/gf/g/container/glist"
    "gitee.com/johng/gf/g/container/gqueue"
    "fmt"
)

// 监听管理对象
type Watcher struct {
    watcher    *fsnotify.Watcher        // 底层fsnotify对象
    events     *gqueue.Queue            // 过滤后的事件通知，不会出现重复事件
    closeChan  chan struct{}            // 关闭事件
    callbacks  *gmap.StringInterfaceMap // 监听的回调函数
}

// 监听事件对象
type Event struct {
    Path string      // 文件绝对路径
    Op   Op          // 触发监听的文件操作
    Watcher *Watcher // 时间对应的监听对象
}

// 按位进行识别的操作集合
type Op uint32

const (
    CREATE Op = 1 << iota
    WRITE
    REMOVE
    RENAME
    CHMOD
)

// 全局监听对象，方便应用端调用
var watcher, _ = New()

// 创建监听管理对象
func New() (*Watcher, error) {
    if watch, err := fsnotify.NewWatcher(); err == nil {
        w := &Watcher {
            watcher    : watch,
            events     : gqueue.New(),
            closeChan  : make(chan struct{}),
            callbacks  : gmap.NewStringInterfaceMap(),
        }
        w.startWatchLoop()
        w.startEventLoop()
        return w, nil
    } else {
        return nil, err
    }
}

// 添加对指定文件/目录的监听，并给定回调函数；如果给定的是一个目录，默认递归监控。
func Add(path string, callback func(event *Event), recursive...bool) error {
    if watcher == nil {
        return errors.New("global watcher creating failed")
    }
    return watcher.Add(path, callback, recursive...)
}

// 移除监听，默认递归删除。
func Remove(path string) error {
    if watcher == nil {
        return errors.New("global watcher creating failed")
    }
    return watcher.Remove(path)
}

// 关闭监听管理对象
func (w *Watcher) Close() {
    w.watcher.Close()
    w.events.Close()
    close(w.closeChan)
}

// 添加对指定文件/目录的监听，并给定回调函数
func (w *Watcher) addWatch(path string, callback func(event *Event)) error {
    // 这里统一转换为当前系统的绝对路径，便于统一监控文件名称
    t := gfile.RealPath(path)
    if t == "" {
        return errors.New(fmt.Sprintf(`"%s" does not exist`, path))
    }
    path = t
    // 注册回调函数
    w.callbacks.LockFunc(func(m map[string]interface{}) {
        var result interface{}
        if v, ok := m[path]; !ok {
            result  = glist.New()
            m[path] = result
        } else {
            result = v
        }
        result.(*glist.List).PushBack(callback)
    })
    // 添加底层监听
    w.watcher.Add(path)
    return nil
}

// 添加监控，path参数支持文件或者目录路径，recursive为非必需参数，默认为递归添加监控(当path为目录时)
func (w *Watcher) Add(path string, callback func(event *Event), recursive...bool) error {
    if gfile.IsDir(path) && (len(recursive) == 0 || recursive[0]) {
        paths, _ := gfile.ScanDir(path, "*", true)
        list  := []string{path}
        list   = append(list, paths...)
        for _, v := range list {
            if err := w.addWatch(v, callback); err != nil {
                return err
            }
        }
        return nil
    } else {
        return w.addWatch(path, callback)
    }
}


// 移除监听
func (w *Watcher) removeWatch(path string) error {
    w.callbacks.Remove(path)
    return w.watcher.Remove(path)
}

// 递归移除监听
func (w *Watcher) Remove(path string) error {
    if gfile.IsDir(path) {
        paths, _ := gfile.ScanDir(path, "*", true)
        list := []string{path}
        list  = append(list, paths...)
        for _, v := range list {
            if err := w.removeWatch(v); err != nil {
                return err
            }
        }
        return nil
    } else {
        return w.removeWatch(path)
    }
}

// 监听循环
func (w *Watcher) startWatchLoop() {
    go func() {
        for {
            select {
                // 关闭事件
                case <- w.closeChan:
                    return

                // 监听事件
                case ev := <- w.watcher.Events:
                    //glog.Debug("gfsnotify: watch loop", ev)
                    w.events.Push(&Event{
                        Path : ev.Name,
                        Op   : Op(ev.Op),
                    })

                case err := <- w.watcher.Errors:
                    glog.Error("error : ", err);
            }
        }
    }()
}

// 检索给定path的回调方法**列表**
func (w *Watcher) getCallbacks(path string) *glist.List {
    for path != "/" {
        if l := w.callbacks.Get(path); l != nil {
            return l.(*glist.List)
        } else {
            path = gfile.Dir(path)
        }
    }
    return nil
}

// 事件循环
func (w *Watcher) startEventLoop() {
    go func() {
        for {
            if v := w.events.Pop(); v != nil {
                //glog.Debug("gfsnotidy: event loop", v)
                event := v.(*Event)
                if event.IsRemove() {
                    if gfile.Exists(event.Path) {
                        // 如果是文件删除事件，判断该文件是否存在，如果存在，那么将此事件认为“假删除”，
                        // 并重新添加监控(底层fsnotify会自动删除掉监控，这里重新添加回去)
                        w.watcher.Add(event.Path)
                        // 修改时间操作为重命名(相当于重命名为自身名称，最终名称没变)
                        event.Op = RENAME
                    } else {
                        // 如果是真实删除，那么递归删除监控信息
                        w.Remove(event.Path)
                    }
                }
                //glog.Debug("gfsnotidy: event loop callbacks", v)
                callbacks := w.getCallbacks(event.Path)
                // 如果创建了新的目录，那么将这个目录递归添加到监控中
                if event.IsCreate() && gfile.IsDir(event.Path) {
                    for _, callback := range callbacks.FrontAll() {
                        w.Add(event.Path, callback.(func(event *Event)))
                    }
                }
                if callbacks != nil {
                    go func(callbacks *glist.List) {
                        for _, callback := range callbacks.FrontAll() {
                            callback.(func(event *Event))(event)
                        }
                    }(callbacks)
                }
            } else {
                break
            }
        }
    }()
}