<div align="center">
<img alt="Preview" src="./public/contextify.png" width="100%" style="border-radius: 8px">

# 🤖 Contextify - AI 代码上下文提取工具
</div>

<p align="center">
  <a href="https://golang.org/"><img src="https://img.shields.io/badge/Go-1.23+-00ADD8.svg?style=for-the-badge&logo=go" alt="Go Version"></a>
  <a href="#"><img src="https://img.shields.io/badge/License-MIT-blue.svg?style=for-the-badge" alt="License"></a>
  <a href="#"><img src="https://img.shields.io/github/v/release/EthanGuo-coder/Contextify?style=for-the-badge" alt="Release"></a>
</p>

有没有过这样的体验：想把一堆代码丢进 ChatGPT、Claude 或 Gemini，结果复制粘贴半天，最后上下文还丢了一半？😩 我们也深有体会。

**Contextify** 就是为解决这个痛点而生的！它是一个轻量的命令行工具，可以自动扫描项目目录，智能筛选重要内容，把它们打包成一个整洁的上下文文件，直接喂给 AI，回答效果立马提升 🚀。

---

## ✨ 功能亮点

* 🧠 **智能上下文提取**：自动遍历项目目录，理解项目结构。
* 📝 **多种输出格式**：支持 `Markdown`、`JSON`、`YAML`。
* 🚫 **智能过滤**：自动识别 `.gitignore`，自带常见无用目录过滤（如 `node_modules`、`build` 等），还能通过 `--exclude`/`--include` 自定义。
* ✂️ **代码瘦身**：使用 `--strip-comments` 快速去掉注释，节省 token。
* 💰 **按 Token 限制输出**：通过 `--max-tokens` 限定大小，超出的部分会智能裁剪。
* 🔬 **Go AST 分析**：`--ast` 可解析 Go 文件，输出包、导入、结构体和函数等概要信息。
* 🎯 **聚焦模式**：用 `--focus "函数名"` 直击目标函数及相关上下文，AI 调试更高效。
* ⚡ **高性能**：并发处理文件，提取速度飞快。
* ⚙️ **高度可配置**：既能用命令行参数，也能写配置文件 `.ai-context.yaml` 固化规则。

---

## 🚀 安装

目前推荐从源码编译安装，保证拿到最新版本。

### 第一步：克隆仓库
```bash
git clone https://github.com/EthanGuo-coder/Contextify.git
cd Contextify
```

### 第二步：编译可执行文件
```bash
make build
```
这会生成 `contextify` 可执行文件。

### 第三步：添加到系统 PATH（推荐）

**macOS / Linux**：
```bash
sudo mv ./contextify /usr/local/bin/
```

**Windows**：
1. 新建一个目录（如 `C:\Program Files\GoTools`）。
2. 把 `contextify.exe` 放进去。
3. 打开「系统环境变量」，编辑 `Path`，新增该目录。
4. 保存并重启终端。

这样就能在任何目录直接执行 `contextify`。

---

## 🛠️ 使用方法

进入你的项目目录，运行 `extract` 命令即可。

### 基本用法

```bash
contextify extract
```

会生成一个类似 `contextify-20250907_102856.md` 的文件。

### 常用参数

```bash
# 指定项目目录和输出文件
contextify extract --path ./my-awesome-project --output context.md

# 输出 JSON 格式
contextify extract --format json

# 去掉注释
contextify extract --strip-comments

# 限制 token 数量
contextify extract --max-tokens 8000

# 排除测试文件
contextify extract --exclude "**/*_test.go"
```

### 🎯 进阶用法：AST + Focus（Go 专属）

比如你要调试 `generateMarkdown` 函数，可以这样：

```bash
contextify extract --ast --focus "generateMarkdown" --depth 1 --output markdown_context.md
```

它会分析 Go 代码，找到目标函数和相关调用链，并优先收集这些文件，生成极具针对性的上下文。

---

## ⚙️ 配置文件

你可以在项目根目录新建 `.ai-context.yaml`，让 Contextify 自动读取配置。命令行参数的优先级高于配置文件。

示例：
```yaml
# 输出格式：markdown, json, yaml
format: markdown

# 启用 Go AST
ast: true

# 去掉注释
strip_comments: true

# token 上限（0 表示不限）
max_tokens: 16000

# 排除文件/目录
exclude:
  - "*.test.go"
  - "testdata/*"
  - "build/**"
  - "dist/**"
  - "*.md"

# 仅包含指定路径（可选）
# include:
#   - "pkg/**/*.go"
#   - "internal/**/*.go"
```

---

## 🧑‍💻 参与贡献

欢迎提交 issue 或 PR！

开发辅助命令：
```bash
# 编译
make build

# 跑测试
make test

# 格式化 & Lint
make fmt
make lint

# 清理
make clean
```

---

## 📜 开源协议

本项目基于 **Apache 2.0 协议** 发布，完整协议请见 [LICENSE](https://www.apache.org/licenses/LICENSE-2.0)。
