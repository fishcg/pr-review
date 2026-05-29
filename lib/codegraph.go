package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CodeGraphConfig CodeGraph 集成配置
type CodeGraphConfig struct {
	Enabled       bool     // 是否启用 codegraph
	BinaryPath    string   // codegraph 可执行文件路径，默认 "codegraph"
	IndexTimeout  int      // 建索引超时（秒）
	ExtraInitArgs []string // 额外的 init 参数（如 --include 等）
}

// CodeGraphManager 负责在工作目录中构建 codegraph 索引并生成 MCP 配置
type CodeGraphManager struct {
	cfg CodeGraphConfig
}

// NewCodeGraphManager 创建 codegraph 管理器
func NewCodeGraphManager(cfg CodeGraphConfig) *CodeGraphManager {
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = "codegraph"
	}
	if cfg.IndexTimeout <= 0 {
		cfg.IndexTimeout = 600
	}
	return &CodeGraphManager{cfg: cfg}
}

// Enabled 是否启用
func (m *CodeGraphManager) Enabled() bool {
	return m != nil && m.cfg.Enabled
}

// CheckAvailable 校验 codegraph 二进制是否可用
func (m *CodeGraphManager) CheckAvailable() error {
	cmd := exec.Command(m.cfg.BinaryPath, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("codegraph not available at %s: %w, output: %s",
			m.cfg.BinaryPath, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// BuildIndex 在 workDir 中执行 `codegraph init -i`（同步建图）
// 返回是否成功；失败时调用方应继续主流程，不应因索引失败而终止 review。
func (m *CodeGraphManager) BuildIndex(workDir string) error {
	if !m.Enabled() {
		return nil
	}

	timeout := time.Duration(m.cfg.IndexTimeout) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	args := []string{"init", "-i"}
	args = append(args, m.cfg.ExtraInitArgs...)

	start := time.Now()
	log.Printf("📊 [codegraph] Building index at %s (timeout=%v)", workDir, timeout)

	cmd := exec.CommandContext(ctx, m.cfg.BinaryPath, args...)
	cmd.Dir = workDir
	// 向 codegraph 透传必要的环境变量（npx / node 解析依赖 PATH/HOME）
	cmd.Env = os.Environ()

	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start).Round(time.Millisecond)

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("codegraph index timeout after %v", timeout)
		}
		// 截断输出避免日志膨胀
		preview := strings.TrimSpace(string(out))
		if len(preview) > 1000 {
			preview = preview[:1000] + "...(truncated)"
		}
		return fmt.Errorf("codegraph init failed (elapsed=%v): %w, output: %s", elapsed, err, preview)
	}

	log.Printf("✅ [codegraph] Index built in %v", elapsed)
	return nil
}

// IndexExists 判断 workDir 中是否已经有 .codegraph 索引目录
func (m *CodeGraphManager) IndexExists(workDir string) bool {
	if workDir == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(workDir, ".codegraph"))
	return err == nil && info.IsDir()
}

// ClaudeMCPConfig 为 Claude CLI 生成 --mcp-config 用的 JSON 字符串
//
//	{
//	  "mcpServers": {
//	    "codegraph": {
//	      "type": "stdio",
//	      "command": "codegraph",
//	      "args": ["serve", "--mcp"]
//	    }
//	  }
//	}
func (m *CodeGraphManager) ClaudeMCPConfig() (string, error) {
	if !m.Enabled() {
		return "", nil
	}
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"codegraph": map[string]any{
				"type":    "stdio",
				"command": m.cfg.BinaryPath,
				"args":    []string{"serve", "--mcp"},
			},
		},
	}
	buf, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to marshal codegraph mcp config: %w", err)
	}
	return string(buf), nil
}

// ClaudeAllowedToolNames 返回 Claude 应该自动放行的 codegraph 工具名
func (m *CodeGraphManager) ClaudeAllowedToolNames() []string {
	if !m.Enabled() {
		return nil
	}
	return []string{
		"mcp__codegraph__codegraph_search",
		"mcp__codegraph__codegraph_context",
		"mcp__codegraph__codegraph_callers",
		"mcp__codegraph__codegraph_callees",
		"mcp__codegraph__codegraph_impact",
		"mcp__codegraph__codegraph_node",
		"mcp__codegraph__codegraph_status",
		"mcp__codegraph__codegraph_files",
		"mcp__codegraph__codegraph_explore",
		"mcp__codegraph__codegraph_trace",
	}
}

// CodexConfigArgs 为 Codex CLI 生成 -c key=value 形式的注入参数
// codex 通过 ~/.codex/config.toml 的 [mcp_servers.<name>] 配置 MCP 服务，
// 命令行可以用 -c mcp_servers.codegraph.command="codegraph" 等形式覆盖。
func (m *CodeGraphManager) CodexConfigArgs() []string {
	if !m.Enabled() {
		return nil
	}
	// codex 的 -c 值会被当作 TOML 解析；字符串需要带引号，数组用 TOML 数组语法
	return []string{
		"-c", fmt.Sprintf("mcp_servers.codegraph.command=%q", m.cfg.BinaryPath),
		"-c", `mcp_servers.codegraph.args=["serve", "--mcp"]`,
	}
}

// CodeGraphGuidance 给 CLI 一段简短提示，让模型主动用 codegraph 工具
func CodeGraphGuidance() string {
	return strings.Join([]string{
		"📚 本仓库已构建 CodeGraph 索引（位于 .codegraph/）。",
		"在做以下场景时请优先使用 mcp__codegraph__* 工具，而不是 Grep/Read 大量探索：",
		"  - 查找符号定义、调用方/被调用方：codegraph_search / codegraph_callers / codegraph_callees",
		"  - 评估某次变更的影响半径：codegraph_impact",
		"  - 快速读取符号源码：codegraph_node",
		"  - 在大块陌生代码区域建立心智模型：codegraph_context / codegraph_explore",
		"如果 codegraph 工具返回了源码片段，可视为已读，无需再 Read 一次。",
	}, "\n")
}
