# Tunnelchat (v2)

[English](README.md) | [中文](README_zh.md)

**Tunnelchat v2** 是命令行 P2P 聊天工具。双方先钉扎长期身份，再收集候选地址并双向探测，然后做 **Noise_IK** 握手（X25519 + ChaCha20-Poly1305 + SHA-256，并用 Ed25519 绑定身份）。没有通往旧版 DES/MD5 协议的静默降级。

本实现仍在本地演进。测试覆盖的是 **本机回环以及用户态 NAT/STUN/DHT/中继模拟**，不是真实的双重 NAT 或 CGNAT。单元测试通过不等于公网一定能打通。

---

## v2 变化

* **身份：** 随机 32 字节种子派生 Ed25519（签名/钉扎）和 X25519（Noise 静态键）。密码不再作为密钥材料。
* **会话：** `Noise_IK_25519_ChaChaPoly_SHA256`，固定 prologue。传输层带滑动重放窗口。被篡改或重放的包会丢弃。DHT 上的地址在钉扎探测与握手成功前不可信。
* **共用 UDP 口：** STUN、可选 DHT、探测、会话、可选中继共用一个套接字，**STUN 映射端口就是聊天端口**。映射会定期刷新，不会把配置里的旧 IP 当成永久地址。
* **候选：** 本机 IPv4/IPv6 与 STUN 反射地址。邀请串是带外分发，可以带局域网提示。**公网 DHT 记录不放明文私网地址**；可选的 BEP44 风格提示先加密，再只保留公网端点。
* **DHT：** `online` **默认启用 Mainline DHT 发现**（使用文档中列出的公共引导节点，可覆盖或关闭）。**DHT 只用于发现**，不是聊天中继，也不能单独打洞。消息载荷仍端到端加密（Noise/AEAD）。上线会把公网 IP 暴露给引导节点和 DHT 节点。通过 v2 公共 swarm 和 **Ed25519 签名的在线记录**（显示名与公钥绑定）可以列出当前在线的陌生人，**不会自动写入可信好友列表**。不能把 DHT 上的 IP 直接当成身份。未签名、过期或伪造的在线记录会被忽略。明文公共记录不放私网地址。
* **中继：** 可选、由运行者自己启动的 UDP 转发。载荷对中继不透明。用 token MAC 做访问控制，有会话上限和空闲回收。默认听在回环地址。这不是必备中心服务器。

---

## 不兼容与迁移

v2 **不能与 v1**（DES-ECB / MD5 / `tunnelchat.xml`）互通。已有 v1 XML **只检测、不改写**。`new` 不会覆盖已有 v2 文件，也拒绝 `.xml` 路径。请用新的 JSON 路径（默认 `tunnelchat.json`）创建新身份。没有自动导入旧账号或好友密钥。

---

## 编译

需要 Go 1.26 或更高：

```bash
make build
make test
```

测试使用进程内假 STUN/DHT/中继和用户态 NAT，**不会**向公网 Mainline DHT 宣布。

---

## 用法

```bash
./bin/tunnelchat -c alice.json new Alice
./bin/tunnelchat -c bob.json new Bob
./bin/tunnelchat -c alice.json info
./bin/tunnelchat -c bob.json add 'TC2INV1....'
./bin/tunnelchat -c alice.json online
./bin/tunnelchat -c bob.json online
```

`online` 内：`<名字> <消息>`，`/friends`，`/peers`（或 `/who`），`/connect <指纹>`，`/addpeer <指纹>`，`/info`，`/add <邀请>`，`q`。

DHT 发现（`online` 默认开启）：

```bash
./bin/tunnelchat -c alice.json online
./bin/tunnelchat -c alice.json --no-dht online
# 或在 JSON 中设置 { "dht": { "disabled": true } }
./bin/tunnelchat -c alice.json --dht-bootstrap 127.0.0.1:6881 online
```

默认引导节点：`router.bittorrent.com:6881`、`dht.transmissionbt.com:6881`、`router.utorrent.com:6881`、`dht.libtorrent.org:25401`。`info` 不会向 DHT 宣布。单元测试不会套用这份默认列表，因此不会访问公网。

双方都 `online` 且 DHT 开启时，`/peers` 或 `/who` 会列出发现到的陌生人（显示名与长期 Ed25519 公钥一起被签名）。任何人都可以起名「Alice」，**请核对指纹**。在线记录约五分钟过期，也可能缺失或被污染。`/connect <指纹>` 做加密握手但不会自动加好友；`/addpeer <指纹>` 才会钉扎到好友列表。邀请串仍然可用。这不是匿名工具：别人能看到有 tunnelchat 客户端在你的公网 IP 上在线。

可选中继：

```bash
./bin/tunnelchat relay --listen 127.0.0.1:9000 --token '长随机口令'
./bin/tunnelchat -c alice.json --relay-server 127.0.0.1:9000 --relay-token '长随机口令' online
```

中继运营者能看到 IP 与时序，看不到消息明文。不要在未评估风险的情况下挂无人值守的公网中继。

---

## 限制（请先读）

* **不保证能穿透「子局域网」。** 同一子网在有局域网候选或 hairpin 可用时通常可以。跨子网还要有路由和 UDP 放行。访客隔离、双重 NAT、CGNAT、对称 NAT 在没有中继时经常失败。已实现同时探测，**没有端口预测**。本仓库**没有**做真实双重 NAT/CGNAT 实测。
* **开启 DHT 的 `online` 会把公网 IP 暴露**给引导/DHT 节点以及查询公共 swarm 的人。这不是匿名工具。聊天内容仍端到端加密，但 IP 与在线状态会暴露。
* **陌生人显示名不唯一。** 签名把名字绑在密钥上，但任何人都可以自称「Alice」。请核对指纹。
* **DHT 记录会过期、可能缺失或被污染。** 在线记录 TTL 很短。DHT 仍然不能打洞；困难 NAT 仍可能需要中继。
* **没有离线收件箱。** 双方必须同时 `online`。
* **IPv6：** 会收集本机候选；主监听仍是 IPv4（`udp4`）。纯 IPv6 收包尚未做完。
* **邀请串**含身份密钥、会合秘密和可选局域网端点，应当能力凭证保管，不要发到公共 DHT 或公开论坛。
* **密钥文件**（JSON 种子）须保持私密（`0600`）。
* **DHT 存储**（含签名可变记录）是尽力而为、有大小和时效限制；并非所有 Mainline 节点都实现 BEP 44。

---

## 目录

见英文 README 的 Project layout 一节。
