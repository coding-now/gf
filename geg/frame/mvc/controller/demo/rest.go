package demo

import (
    "gitee.com/johng/gf/g/frame/gmvc"
    "gitee.com/johng/gf/g"
)

// 测试控制器
type ControllerRest struct {
    gmvc.Controller
}

// 初始化控制器对象，并绑定操作到Web Server
func init() {
    // 控制器公开方法中与HTTP Method方法同名的方法将会绑定映射
    g.HTTPServer().BindControllerRest("/user", &ControllerRest{})
}

// RESTFul - GET
func (c *ControllerUser) Get() {
    c.Response.WriteString("RESTFul HTTP Method GET")
}

// RESTFul - POST
func (c *ControllerUser) Post() {
    c.Response.WriteString("RESTFul HTTP Method POST")
}

// RESTFul - DELETE
func (c *ControllerUser) Delete() {
    c.Response.WriteString("RESTFul HTTP Method DELETE")
}

// 该方法无法映射，将会无法访问到
func (c *ControllerUser) Hello() {
    c.Response.WriteString("Hello")
}



