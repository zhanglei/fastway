介绍
====

[![Go Foundation](https://img.shields.io/badge/go-foundation-green.svg)](http://golangfoundation.org)
[![Go Report Card](https://goreportcard.com/badge/github.com/funny/fastway)](https://goreportcard.com/report/github.com/funny/fastway)
[![Build Status](https://travis-ci.org/funny/fastway.svg?branch=master)](https://travis-ci.org/funny/fastway)
[![codecov](https://codecov.io/gh/funny/fastway/branch/master/graph/badge.svg)](https://codecov.io/gh/funny/fastway)
[![GoDoc](https://img.shields.io/badge/api-reference-blue.svg)](https://godoc.org/github.com/funny/fastway/go)

本网关是一个游戏用网关，它负责客户端和服务端之间的消息转发。

通过网关，我们预期达到以下目的：

+ 减少暴露在公网的服务器
+ 提升网络故障转移的效率
+ 复用客户端网络连接
+ 使服务端主动连接客户端成为可能
+ 不修改服务端的情况下引入断线重连机制和加密机制

说明
====

<p align="center"><img src="https://rawgit.com/funny/fastway/master/README.svg" alt="Gateway" /></p>
<p align="center"><b>图1 - 拓扑结构</b></p>

基本逻辑：

+ 每个系统允许部署任意个网关
+ 每个网关启动时守护对外和对内两个网络端口
+ 游戏服务端启动时主动连接到所有网关的对内端口，并告知网关自己的ID
+ 新的客户端连接产生时，随机连接到一个网关的对外端口
+ 客户端使用服务端ID来建立虚拟连接，每个客户端可以建立多个虚拟连接
+ 网关为每个虚拟连接分配一个ID，并告知服务端有一个新的虚拟连接产生
+ 客户端和服务端拿到虚拟连接ID之后即可使用此虚拟连接进行后续通讯
+ 通讯过程中通过在消息头部附加虚拟连接ID来告知网关此消息的去处
+ 服务端在已知客户端连接ID的情况下，可以主动连接客户端

设计意图和实现细节：

+ 允许部署任意个网关的目的是实现负载均和防单点故障
+ 网关可以开启reuseport特性，提升多核利用率
+ 由服务端主动连接网关目的是倒置依赖性，降低网关复杂度，网关服务注册和发现由用户自定义
+ 客户端具体如何连接到网关可以由用户自定义，可以是根据负载情况或地域分配等
+ 为防止恶意攻击，网关可以限制每个客户端虚拟连接数
+ 网关运用零拷贝和内存池来提升消息处理效率
+ 服务端主动连接客户端时，客户端连接ID可以是RPC获取或Redis存储，具体实现由用户自定义

命令行
=====

本网关提供了一个命令行程序用来对外提供服务。

命令行公共参数：

| 参数名 | 说明 | 默认值 |
| --- | --- | --- |
| ReusePort | 是否开启reuseport特性 | false |
| MaxPacketSize | 最大的消息包体积 | 512K |
| MemPoolType | [slab内存池类型 (sync、atom或chan)](https://github.com/funny/slab) | atom |
| MemPoolFactor | slab内存池的Chunk递增指数 | 2 |
| MemPoolMaxChunk | slab内存池中最大的Chunk大小 | 64K |
| MemPoolMinChunk | slab内存池中最小的Chunk大小 | 64B |
| MemPoolPageSize | slab内存池的每个Slab内存大小 | 1M |

客户端相关参数：

| 参数名 | 说明 | 默认值 |
| --- | --- | --- |
| ClientAddr | 网关暴露给客户端的地址 | ":0" |
| ClientMaxConn | 每个客户端可以创建的最大虚拟连接数 | 16 |
| ClientBufferSize | 每个客户端连接使用的 bufio.Reader 缓冲区大小 | 2K |
| ClientPingInterval | 网关在多少秒没收到客户端消息后发送PING指令给客户端 | 30秒 |
| ClientSendChanSize | 每个客户端连接异步发送消息用的chan缓冲区大小 | 1000 |
| ClientSnetEnable | [是否为客户端开启snet协议](https://github.com/funny/snet) | false |
| ClientSnetEncrypt | 是否为客户端开启snet加密 | false |
| ClientSnetBuffer | 每个客户端物理连接对应的snet重连缓冲区大小 | 32k |
| ClientSnetInitTimeout | 客户端snet握手超时时间 | 10秒 |
| ClientSnetWaitTimeout | 客户端snet等待重连超时时间 | 60秒 |

服务端相关参数：

| 参数名 | 说明 | 默认值 |
| --- | --- | --- |
| ServerAddr | 网关暴露给服务端的地址 | ":0" |
| ServerAuthPassword | 用于验证服务端合法性的秘钥 | 空 |
| ServerAuthTimeout | 验证服务端连接时的最大IO等待时间 | 3秒 |
| ServerBufferSize | 每个服务端连接使用的 bufio.Reader 缓冲区大小 | 64K |
| ServerPingInterval | 网关在多少秒没收到客户端消息后发送PING指令给客户端 | 30秒 |
| ServerSendChanSize | 每个服务端连接异步发送消息用的chan缓冲区大小 | 10万 |
| ServerSnetEnable | [是否为服务端开启snet协议](https://github.com/funny/snet) | false |
| ServerSnetEncrypt | 是否为服务端开启snet加密 | false |
| ServerSnetBuffer | 每个服务端物理连接对应的snet重连缓冲区大小 | 1M |
| ServerSnetInitTimeout | 服务端snet握手超时时间 | 10秒 |
| ServerSnetWaitTimeout | 服务端snet等待重连超时时间 | 60秒 |

API
===

本网关目前支持以下编程语言的调用：

+ [Go版](https://github.com/fast/fastway/tree/master/go)
+ [C#版](https://github.com/fast/fastway/tree/master/csharp)

通讯协议细节请进一步阅读Go调用库的文档。

相关项目
=======

* [通讯协议代码生成](https://github.com/funny/fastbin)
* [slab算法的内存池](https://github.com/funny/slab)
* [支持断线重连和加密的流协议](https://github.com/funny/snet)

参与项目
=======

欢迎提交通过github的issues功能提交反馈或提问。

技术群：474995422