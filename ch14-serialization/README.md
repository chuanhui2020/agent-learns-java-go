# Ch14 · 序列化与服务集成 ⭐

> 对应路线图 **阶段 5**。状态：⚪ 未开始（待带教）。**需额外安装**：`protoc`、`redis`。

## 🎯 目标
- protobuf + 代码生成（protobuf-go / vtprotobuf 零拷贝）—— 游戏服强烈建议。
- 标准库 `net/http`（自带生产级 HTTP，比 Spring 薄很多但够用）。
- `database/sql` + 驱动、`go-redis`；`etcd client`（服务发现/配置）概览。

## 🔗 前置
Ch13

## 📊 Java 25 → Go 对照表（雏形，带教时展开）
| 主题 | Java 25 | Go |
|---|---|---|
| 序列化 | protobuf / kryo | protobuf-go / vtprotobuf、gob、msgpack、flatbuffers |
| HTTP | Spring 全家桶 | `net/http`（薄但够） |
| DB | JDBC | `database/sql` + 驱动 |
| Redis | Jedis / Lettuce | `go-redis` |

## 🔧 安装（推进到本章时）
```bash
brew install protobuf        # protoc
brew install redis           # 本地 redis
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
```

## 📝 带教时会补
讲解笔记 · `go/demo`（protobuf 编解码 + http + go-redis 可运行）· `go/exercises` · `java/`（protobuf 对照）。
