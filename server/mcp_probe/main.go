// Command mcp_probe 验证外部 MCP 服务器连通性（以高德 MCP 为例）。
//
// 用法：
//
//	export AMAP_MCP_KEY=你的高德Key
//	go run ./mcp_probe
//
// 脚本会先发送 initialize 握手、再调用 tools/list，打印高德 MCP 暴露的工具清单。
// 验证通过后，即可在「智能体 → MCP 配置」里同源新建一条 AgentMCPServer 配置投入使用。
package main

import (
	"fmt"
	"os"

	"aiagent/internal/mcpclient"
	"aiagent/internal/model"
)

func main() {
	key := os.Getenv("AMAP_MCP_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "请先设置环境变量 AMAP_MCP_KEY（高德开放平台申请的 Key）")
		os.Exit(1)
	}

	srv := &model.AgentMCPServer{
		Name:      "amap",
		Transport: model.MCPTransportStreamableHTTP,
		URL:       "https://mcp.amap.com/mcp?key=" + key,
	}

	tools, err := mcpclient.NewClient().ListTools(srv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "连接高德 MCP 失败:", err)
		os.Exit(1)
	}

	fmt.Printf("连接成功，共 %d 个工具：\n\n", len(tools))
	for _, t := range tools {
		fmt.Printf("- %s\n  %s\n", t.Name, t.Description)
	}
}
