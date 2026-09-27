# Tunnelchat (Go Edition)

[English](README.md) | [中文](README_zh.md)

**Tunnelchat** 是一款去中心化、基于 STUN 穿透与端到端加密的 P2P 命令行聊天工具。已完全重构为现代化 Go 内核，移除遗留的 Qt UI 与 C++ 编译依赖，提供跨平台且开箱即用的 Linux 命令行交互体验。

---

## 特性

* **无需中央服务器**：纯 P2P 架构，消息点对点直连传输
* **STUN NAT 穿透**：纯 Go 实现 RFC 3489 / RFC 5389 协议，支持探测公网映射地址与 NAT 类型
* **局域网/离线自动降级**：STUN 服务器不可用时自动回退至本地网络 IP，局域网与公网无缝自适应
* **端到端加密**：全链路采用 DES-ECB + MD5 双向密钥加密传输，好友间独立协商密钥
* **心跳与保活打洞**：定时发送 UDP 心跳维持 NAT 映射状态
* **状态实时同步**：自动同步好友 IP 与端口变更
* **Linux 交互式命令行**：内置好友列表查看、实时收发、连接码一键生成等命令

---

## 快速开始

### 1. 编译构建

要求 Go 1.20 或更高版本：

```bash
# 使用 Makefile
make build

# 或使用构建脚本
./build.sh

# 运行测试
make test
```

编译生成的可执行文件位于 `bin/tunnelchat`。

---

### 2. 命令列表

```bash
tunnelchat <command> [arguments]
```

| 命令 | 参数 | 说明 |
| :--- | :--- | :--- |
| `new` | `<name> <password>` | 创建或重置用户账户 |
| `info` | 无 | 获取加密的用户连接串（发送给好友添加） |
| `add` | `<info_string>` | 通过好友的连接串添加好友并完成握手 |
| `online` | 无 | 进入交互式 Linux 命令行聊天模式 |

---

### 3. 使用流程示例

#### 步骤 1：创建用户

用户 A：
```bash
./bin/tunnelchat new Alice 123456
```

用户 B：
```bash
./bin/tunnelchat new Bob 654321
```

#### 步骤 2：获取连接信息并互加好友

用户 A 获取自身信息串：
```bash
./bin/tunnelchat info
```
输出形如：
```
My Information:
  Name: Alice
  Account: 4B3C18DE30FDE73A393D43E085B88350
  Address: 162.62.119.173:53714

Share this info string with your friends to add you:
98F8E4DED6E5EEF52D81640FCA8135EF3960FEF370972992...
```

用户 B 添加用户 A：
```bash
./bin/tunnelchat add 98F8E4DED6E5EEF52D81640FCA8135EF3960FEF370972992...
```

同理，用户 A 添加用户 B 的信息串完成双向密钥协商。

#### 步骤 3：进入交互式聊天模式

启动在线聊天模式：
```bash
./bin/tunnelchat online
```

在交互式命令行中：
* 发送消息：直接输入 `<好友名字> <消息内容>`（例如：`Bob 你好！今天怎么样？`）
* 查看好友列表：输入 `/friends` 或 `/list`
* 查看自身信息：输入 `/info`
* 在线添加好友：输入 `/add <info_string>`
* 退出聊天：输入 `q` 或 `quit`

---

## 目录结构

```
.
├── cmd/
│   └── tunnelchat/     # 命令行入口
├── pkg/
│   ├── cli/            # 终端交互界面与命令行逻辑
│   ├── config/         # XML 配置加载与好友/用户数据管理
│   ├── core/           # P2P 通信引擎（UDP、RPC、心跳保活、数据分发）
│   ├── crypto/         # 密码学组件（DES 加解密、MD5、GUID）
│   └── stun/           # 纯 Go STUN 客户端与网络探测
├── bin/                # 编译二进制输出目录
├── build.sh            # 快捷构建脚本
├── Makefile            # Makefile 构建规则
├── README.md           # 英文说明文档
└── README_zh.md        # 中文说明文档
```
