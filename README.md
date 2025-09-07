![](./main_img.png)

# 🤖 Contextify - AI Code Context Extractor

<p align="center">
  <a href="https://golang.org/"><img src="https://img.shields.io/badge/Go-1.23+-00ADD8.svg?style=for-the-badge&logo=go" alt="Go Version"></a>
  <a href="#"><img src="https://img.shields.io/badge/License-MIT-blue.svg?style=for-the-badge" alt="License"></a>
  <a href="#"><img src="https://img.shields.io/github/v/release/EthanGuo-coder/Contextify?style=for-the-badge" alt="Release"></a>
</p>

Ever find yourself wrestling with copy-pasting endless files into ChatGPT, Claude, or Gemini, only to lose half the important context? 😩 Yeah, we've been there.

**Contextify** is your new best friend! It's a slick command-line tool that scans your project, intelligently picks out the important stuff, and bundles it all into a single, clean context file, perfectly optimized for AI prompts. Stop explaining your code and start getting better answers! 🚀

## ✨ Features

* 🧠 **Smart Context Extraction**: Automatically walks your project tree to understand its structure.
* 📝 **Multiple Formats**: Generate your context as beautiful `Markdown`, structured `JSON`, or clean `YAML`.
* 🚫 **Intelligent Filtering**: Automatically respects your `.gitignore` and comes with a hefty list of default ignores for common junk (`node_modules`, `build`, etc.). Fine-tune with your own `--exclude` and `--include` patterns!
* ✂️ **Code Distillation**: Use `--strip-comments` to get right to the point and save precious tokens.
* 💰 **Token-Aware Trimming**: Set a `--max-tokens` limit, and Contextify will heuristically trim less important files to fit your budget.
* 🔬 **Go AST Analysis** (Go-specific): Enable `--ast` to get a high-level summary of packages, imports, structs, and functions for your Go files.
* 🎯 **Focus Mode** (Go-specific): This is the magic wand! Zero in on a specific function or method with `--focus "MyFunction"` to trace its definition and related code, ensuring the most relevant context is included.
* ⚡ **Blazingly Fast**: Processes your files concurrently to get you that context ASAP.
* ⚙️ **Super Configurable**: Use command-line flags for quick tasks or drop a `.ai-context.yaml` file in your project for consistent, repeatable results.

## 🚀 Installation

You can easily build from the source to get the latest version.

**Step 1: Clone the Repository**
First, clone the project from GitHub to your local machine.
```bash
git clone https://github.com/EthanGuo-coder/Contextify.git
cd Contextify
```

**Step 2: Build the Binary**
Use the included `Makefile` to compile the project. This will create an executable file named `contextify`.
```bash
make build
```

**Step 3: Add to Your System PATH (Recommended)**
To run `contextify` from anywhere, move the binary to a directory in your system's PATH. This makes it super convenient!

For **macOS or Linux**:
```bash
sudo mv ./contextify /usr/local/bin/
```
Now you can run `contextify` in any terminal.

For **Windows**:
1.  Create a folder where you want to keep your command-line tools (e.g., `C:\Program Files\GoTools`).
2.  Move the `contextify.exe` file into that folder.
3.  Search for "Edit the system environment variables" in the Start Menu and open it.
4.  Click on the "Environment Variables..." button.
5.  Under "System variables", find and select the `Path` variable, then click "Edit...".
6.  Click "New" and add the path to your new folder (e.g., `C:\Program Files\GoTools`).
7.  Click OK on all windows to save. You may need to restart your terminal for the changes to take effect.

## 🛠️ How to Use

Using Contextify is a piece of cake. Just navigate to your project directory and run the `extract` command.

### Basic Usage

Generate a markdown context file from the current directory:
```bash
contextify extract
```
This will create a file like `contextify-20250907_102856.md` in your project folder.

### Common Flags

Customize the output to your heart's content!
```bash
# Specify a project path and an output file
contextify extract --path ./my-awesome-project --output context.md

# Generate a JSON output instead
contextify extract --format json

# Strip all comments to save tokens
contextify extract --strip-comments

# Set a token limit (e.g., for GPT-4's 8k context)
contextify extract --max-tokens 8000

# Exclude the test files
contextify extract --exclude "**/*_test.go"
```

### 🎯 Power-User Mode: Focus & AST (for Go)

This is where Contextify truly shines for Go developers. Let's say you're debugging the `generateMarkdown` function. You can ask Contextify to build a context specifically around it.
```bash
# Create a context focused on the 'generateMarkdown' function and its direct connections
contextify extract --ast --focus "generateMarkdown" --depth 1 --output markdown_context.md
```
Contextify will analyze the Go code, find `generateMarkdown` and any functions it calls or that call it (within the specified depth), and then prioritize those files when building the context. It's like having a surgical tool for context creation!

## ⚙️ Configuration File

For project-specific settings, create a `.ai-context.yaml` file in your project's root directory. Contextify will automatically pick it up. CLI flags will always override the settings in this file.

Here’s an example `.ai-context.yaml`:
```yaml
# Output format: markdown, json, or yaml
format: markdown

# Enable Go AST analysis
ast: true

# Strip comments to reduce token usage
strip_comments: true

# Maximum estimated tokens (0 for unlimited)
max_tokens: 16000

# Files and directories to exclude
# These are added to the default ignore list
exclude:
  - "*.test.go"
  - "testdata/*"
  - "build/**"
  - "dist/**"
  - "*.md"

# If specified, only these patterns will be included
# include:
#   - "pkg/**/*.go"
#   - "internal/**/*.go"
```

## 🧑‍💻 Contributing & Development

Got ideas? Found a bug? We'd love your help! Feel free to open an issue or submit a pull request.

The project includes a `Makefile` to make development easier.
```bash
# Build the binary
make build

# Run tests
make test

# Format and lint the code
make fmt
make lint

# Clean up build artifacts
make clean
```

## 📜 License

This project is licensed under the **Apache 2.0 License**. You can view the full license text [here](https://www.apache.org/licenses/LICENSE-2.0).
