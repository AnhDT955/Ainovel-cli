# ainovel-cli — Bản tiếng Việt

> **Fork tiếng Việt** của [voocel/ainovel-cli](https://github.com/voocel/ainovel-cli) — toàn bộ giao diện, prompt hệ thống và tài liệu đã được Việt hoá.

Công cụ CLI sáng tác tiểu thuyết dài kỳ hoàn toàn tự động bằng AI. Từ **một câu yêu cầu** đến **tiểu thuyết hoàn chỉnh** — không cần can thiệp thủ công trong quá trình viết.

<p align="center">
  <img src="scripts/sample.gif" alt="ainovel-cli demo" width="800">
</p>

---

## Mục lục

1. [Tính năng nổi bật](#tính-năng-nổi-bật)
2. [Kiến trúc hệ thống](#kiến-trúc-hệ-thống)
3. [Yêu cầu hệ thống](#yêu-cầu-hệ-thống)
4. [Cài đặt nhanh](#cài-đặt-nhanh)
   - [Docker (khuyến nghị)](#docker-khuyến-nghị)
   - [Build từ source](#build-từ-source)
5. [Cấu hình](#cấu-hình)
   - [Ollama (local, miễn phí)](#ollama-local-miễn-phí)
   - [OpenRouter](#openrouter)
   - [Anthropic / OpenAI](#anthropic--openai)
   - [Cấu hình nhiều model theo vai trò](#cấu-hình-nhiều-model-theo-vai-trò)
6. [Bắt đầu viết](#bắt-đầu-viết)
7. [Lệnh TUI](#lệnh-tui)
8. [Can thiệp thời gian thực](#can-thiệp-thời-gian-thực)
9. [Phong cách và quy tắc tuỳ chỉnh](#phong-cách-và-quy-tắc-tuỳ-chỉnh)
10. [Khôi phục sau gián đoạn](#khôi-phục-sau-gián-đoạn)
11. [Cấu trúc thư mục output](#cấu-trúc-thư-mục-output)
12. [Xuất truyện](#xuất-truyện)
13. [Troubleshooting](#troubleshooting)

---

## Tính năng nổi bật

| Tính năng | Mô tả |
|---|---|
| **Đa agent tự chủ** | Điều phối viên điều phối Kiến trúc sư → Người viết → Biên tập viên trong một vòng lặp LLM duy nhất |
| **Viết hoàn chỉnh không cần trực** | Nhập một câu, hệ thống tự xây dựng thế giới, lập đề cương, viết và đánh giá toàn bộ |
| **Hỗ trợ 500+ chương** | Quản lý ngữ cảnh 3 tầng (chương → cung → tập), tự động nén khi đầy |
| **Kế hoạch cuộn** | Không lập kế hoạch toàn bộ một lần. Cung/tập sau mở rộng dần khi cần, tránh đề cương rỗng tuếch |
| **Khôi phục cấp step** | Checkpoint sau mỗi lần gọi công cụ thành công — crash/mất mạng/Ctrl+C đều khôi phục chính xác |
| **Can thiệp thời gian thực** | Nhập ý kiến chỉnh sửa bất cứ lúc nào, không cần dừng, hệ thống tự đánh giá phạm vi ảnh hưởng |
| **Đánh giá 7 chiều** | Biên tập viên đánh giá: tính nhất quán, nhân vật, nhịp truyện, mạch kể, phục bút, điểm móc, thẩm mỹ |
| **Chống văn phong AI** | Bộ tiêu chí cơ học + ngữ nghĩa tích hợp sẵn, tự động kiểm tra khi lưu chương |
| **Nhiều nhà cung cấp LLM** | OpenRouter, Anthropic, Gemini, OpenAI, Deepseek, Ollama (local), và proxy tuỳ chỉnh |

---

## Kiến trúc hệ thống

```
┌──────────────────────────────────────────────────┐
│                  Host (vỏ mỏng)                   │
│   Khởi động / Khôi phục / Quan sát / Can thiệp    │
└───────────────────────┬──────────────────────────┘
                        │ Một lần Prompt
┌───────────────────────▼──────────────────────────┐
│          Coordinator (vòng lặp dài LLM)           │
│  Đọc ngữ cảnh → Gọi agent phụ → Đọc kết quả → Tiếp tục │
└────┬───────────┬──────────┬──────────────────────┘
     │           │          │
┌────▼───┐  ┌───▼───┐  ┌───▼────┐
│Architect│  │Writer │  │ Editor │
└────┬───┘  └───┬───┘  └───┬────┘
     └──────────┼───────────┘
                │ Gọi công cụ (IO + checkpoint)
┌───────────────▼──────────────────────────────────┐
│                    Store                          │
│  Progress / Checkpoint / Outline / Drafts / ...  │
└──────────────────────────────────────────────────┘
```

**Nguyên tắc cốt lõi**: LLM quyết định mọi thứ về nội dung và luồng sáng tác. Host chỉ khởi động, quan sát và cung cấp dữ liệu sự thật. Càng ít code, càng ít chỗ hỏng.

---

## Yêu cầu hệ thống

| Phương pháp | Yêu cầu |
|---|---|
| **Docker** (khuyến nghị) | Docker Desktop ≥ 24, 4 GB RAM trống |
| **Build từ source** | Go ≥ 1.21 |
| **Model AI** | API key từ nhà cung cấp, hoặc Ollama chạy local |

> **Windows**: Khuyến nghị dùng Docker. Không cần cài Go.

---

## Cài đặt nhanh

### Docker (khuyến nghị)

**Bước 1** — Tải source và chuẩn bị thư mục:

```bash
git clone https://github.com/<your-username>/ainovel-cli.git
cd ainovel-cli
mkdir config workspace
```

**Bước 2** — Build Docker image:

```bash
docker build -t ainovel-cli-vi .
```

> Lần đầu mất 2–5 phút do tải Go dependencies. Các lần sau dùng cache, rất nhanh.

**Bước 3** — Tạo file cấu hình `config/config.json` (xem [phần Cấu hình bên dưới](#cấu-hình)).

**Bước 4** — Chạy TUI:

```bash
# Linux / macOS
docker run --rm -it \
  -v "$PWD/config:/root/.ainovel" \
  -v "$PWD/workspace:/workspace" \
  -e TERM=xterm-256color \
  ainovel-cli-vi

# Windows (PowerShell)
docker run --rm -it `
  -v "${PWD}\config:/root/.ainovel" `
  -v "${PWD}\workspace:/workspace" `
  -e TERM=xterm-256color `
  ainovel-cli-vi

# Windows (Command Prompt)
docker run --rm -it -v "%CD%\config:/root/.ainovel" -v "%CD%\workspace:/workspace" -e TERM=xterm-256color ainovel-cli-vi
```

> **Windows Terminal**: Mở tab mới tự động —
> ```powershell
> Start-Process "wt.exe" -ArgumentList "new-tab", "cmd", "/k", 'docker run --rm -it -v "%CD%\config:/root/.ainovel" -v "%CD%\workspace:/workspace" -e TERM=xterm-256color ainovel-cli-vi'
> ```

**Chế độ không giao diện** (headless, chạy trên server):

```bash
docker run --rm \
  -v "$PWD/config:/root/.ainovel" \
  -v "$PWD/workspace:/workspace" \
  ainovel-cli-vi \
  --headless --prompt "Viết tiểu thuyết cung đấu, nhân vật chính là cô lao công xuất thân thấp kém"
```

---

### Build từ source

Cần Go ≥ 1.21:

```bash
git clone https://github.com/<your-username>/ainovel-cli.git
cd ainovel-cli
go build -o ainovel-cli ./cmd/ainovel-cli/

# Linux/macOS
./ainovel-cli

# Windows
ainovel-cli.exe
```

---

## Cấu hình

File cấu hình: `config/config.json` (khi dùng Docker) hoặc `~/.ainovel/config.json`.

### Ollama (local, miễn phí)

Chạy Ollama trên máy tính, không cần API key, không mất phí. Phù hợp để thử nghiệm.

```bash
# Cài Ollama: https://ollama.com
# Kéo model (khuyến nghị ít nhất 12B params)
ollama pull gemma4:12b      # Nhẹ, chất lượng tốt
ollama pull qwen3.5:27b     # Chất lượng cao hơn, cần RAM nhiều hơn
```

`config/config.json`:
```json
{
  "provider": "ollama",
  "model": "gemma4:12b",
  "providers": {
    "ollama": {
      "base_url": "http://host.docker.internal:11434/v1",
      "models": ["gemma4:12b", "qwen3.5:27b"]
    }
  }
}
```

> `host.docker.internal` là địa chỉ máy chủ từ container Docker. Trên Linux, thay bằng `172.17.0.1` hoặc địa chỉ IP thực của máy.

> **Lưu ý về Ollama**: Model nhỏ (<14B) có thể gặp khó khăn trong việc tuân theo protocol phức tạp của Coordinator. Khuyến nghị dùng model ≥ 14B hoặc model chuyên về instruction-following (Qwen, Gemma).

---

### OpenRouter

Truy cập [openrouter.ai](https://openrouter.ai) để đăng ký API key. Hỗ trợ hầu hết model lớn (Gemini, Claude, GPT-4, v.v.).

```json
{
  "provider": "openrouter",
  "model": "google/gemini-2.5-flash",
  "providers": {
    "openrouter": {
      "api_key": "sk-or-v1-...",
      "base_url": "https://openrouter.ai/api/v1",
      "models": [
        "google/gemini-2.5-flash",
        "google/gemini-2.5-pro",
        "anthropic/claude-sonnet-4"
      ]
    }
  }
}
```

---

### Anthropic / OpenAI

```json
{
  "provider": "anthropic",
  "model": "claude-sonnet-4-6",
  "providers": {
    "anthropic": {
      "api_key": "sk-ant-...",
      "models": ["claude-sonnet-4-6", "claude-opus-4"]
    }
  }
}
```

---

### Cấu hình nhiều model theo vai trò

Dùng model mạnh cho Kiến trúc sư (lập đề cương), model nhanh cho Người viết (viết chương):

```json
{
  "provider": "openrouter",
  "model": "google/gemini-2.5-flash",
  "providers": {
    "openrouter": {
      "api_key": "sk-or-v1-...",
      "base_url": "https://openrouter.ai/api/v1"
    },
    "anthropic": {
      "api_key": "sk-ant-..."
    }
  },
  "roles": {
    "coordinator": { "provider": "openrouter", "model": "google/gemini-2.5-flash" },
    "architect":   { "provider": "anthropic",  "model": "claude-sonnet-4-6" },
    "writer":      { "provider": "openrouter", "model": "google/gemini-2.5-flash" },
    "editor":      { "provider": "openrouter", "model": "google/gemini-2.5-flash" }
  }
}
```

---

## Bắt đầu viết

1. Khởi động app (xem [phần Cài đặt](#cài-đặt-nhanh))
2. Giao diện TUI hiện ra — nhập yêu cầu tiểu thuyết vào ô bên dưới và nhấn **Enter**

**Ví dụ yêu cầu ngắn** (hệ thống tự bổ sung thêm chi tiết):
```
Cung đấu gia tộc, nhân vật chính xuất thân thấp kém nhưng tài năng ẩn giấu
```

**Ví dụ yêu cầu chi tiết** (kiểm soát nhiều hơn):
```
Tiểu thuyết fantasy cổ đại Việt Nam, nhân vật chính là cô gái nghèo bị bán vào phủ làm tỳ nữ.
Thể loại: cung đấu + ngôn tình. Khoảng 80 chương. Nhân vật nam chính: vương gia lạnh lùng nhưng
thực ra bảo vệ nàng từ bóng tối. Kết thúc có hậu. Viết theo phong cách truyện ngôn tình Việt.
```

> **Mẹo**: Yêu cầu càng cụ thể (thể loại, số chương, nhân vật, tone văn) thì kết quả càng sát ý muốn. Nhưng chỉ một câu ngắn cũng đủ để bắt đầu.

Sau khi nhập, hệ thống bắt đầu tự động:
1. **Kiến trúc sư** — Xây dựng thế giới quan, nhân vật, đề cương chi tiết
2. **Người viết** — Viết từng chương theo đề cương (plan → draft → kiểm tra → lưu)
3. **Biên tập viên** — Đánh giá chất lượng sau mỗi cung truyện, quyết định viết lại hay tiếp tục

---

## Lệnh TUI

Gõ `/` vào ô nhập liệu để xem danh sách lệnh, hoặc dùng trực tiếp:

| Lệnh | Mô tả |
|---|---|
| `/help` | Xem danh sách lệnh và phím tắt |
| `/model [vai-trò]` | Chuyển model — mở bảng chọn. Ví dụ `/model writer` chuyển riêng model Người viết |
| `/diag` | Báo cáo chẩn đoán: phát hiện vòng lặp, chương bỏ sót, phục bút trì trệ, v.v. |
| `/export` | Xuất truyện ra TXT. Xem [Xuất truyện](#xuất-truyện) |
| `/export ~/ten-truyen.epub` | Xuất ra EPUB (đọc được trên máy đọc sách) |
| `/import <đường-dẫn>` | Nhập tiểu thuyết có sẵn để tiếp tục viết |
| `/simulate` | Tạo hồ sơ phong cách viết từ văn mẫu trong thư mục `simulate/` |
| `/cocreate` | Tạm dừng sáng tác, đồng sáng tác lên kế hoạch giai đoạn tiếp theo |

**Phím tắt:**

| Phím | Chức năng |
|---|---|
| `Enter` | Gửi yêu cầu / can thiệp |
| `Tab` | Chuyển chế độ khởi động (Bắt đầu nhanh ↔ Đồng sáng tác) |
| `Esc` | Xóa ô nhập / thoát panel |
| `Ctrl+C` | Thoát app (tiến độ được lưu, lần sau khôi phục tự động) |

---

## Can thiệp thời gian thực

Trong quá trình sáng tác, nhập ý kiến vào ô phía dưới bất cứ lúc nào và nhấn **Enter** — **không cần dừng**.

```
❯ Đưa đường tình cảm lên sớm hơn, chương 3 nên có cảnh gặp gỡ đầu tiên giữa hai nhân vật chính
```

Hệ thống tự phân loại và xử lý:

| Loại can thiệp | Ví dụ | Hệ thống làm gì |
|---|---|---|
| **Tiếp tục** | "viết tiếp đi", "ok hay đó" | Không làm gì, tiếp tục luồng chính |
| **Điều chỉnh dài hạn** | "sau này tăng tỷ lệ đối thoại", "tiêu đề chỉ dùng tiếng Việt" | Lưu chỉ dẫn bền vững, áp dụng từ chương sau |
| **Điều chỉnh đề cương** | "thêm nhân vật phản diện", "đưa tình tiết X lên chương 5" | Kiến trúc sư cập nhật đề cương |
| **Viết lại chương cũ** | "chương 3 hơi nhạt, viết lại" | Biên tập viên xếp hàng viết lại |
| **Thay đổi cài đặt** | "đổi nhân vật chính thành nữ" | Kiến trúc sư cập nhật hồ sơ nhân vật |

---

## Phong cách và quy tắc tuỳ chỉnh

### Chọn phong cách viết

Thêm vào `config.json`:

```json
{
  "style": "romance"
}
```

Các phong cách có sẵn:
- `default` — Phong cách chung, cân bằng
- `romance` — Ngôn tình, chú trọng cảm xúc và mối quan hệ
- `fantasy` — Kỳ ảo, kiếm hiệp, tiên hiệp
- `suspense` — Bí ẩn, trinh thám, hồi hộp
- `wuxia` — Võ hiệp/huyền huyễn mạng, cẩu đạo, sát phạt quyết đoán, hồn xuyên và cày độ thuần thục; dùng kiến trúc áp lực → lựa chọn → hậu quả để giữ lực kéo chương

### Quy tắc tuỳ chỉnh

Tạo file `.md` trong `~/.ainovel/rules/` (toàn cục) hoặc `.ainovel/rules/` trong thư mục dự án (chỉ cho cuốn này). Viết bằng tiếng Việt tự nhiên:

```markdown
# Quy tắc của tôi

- Nhân vật chính không được quá hoàn hảo, cần có điểm yếu rõ ràng
- Ưu tiên cảm xúc nội tâm qua hành động, hạn chế miêu tả trực tiếp
- Đối thoại phải phân biệt rõ giọng từng nhân vật
- Không dùng cấu trúc "không chỉ... mà còn..."
```

Muốn thêm kiểm tra cứng (số từ, từ cấm), thêm phần front matter:

```markdown
---
chapter_words: 2000-4000
forbidden_phrases:
  - không khỏi
  - bỗng dưng
fatigue_words:
  dường như: 2
  tuy nhiên: 1
---

# Quy tắc của tôi

...
```

---

## Khôi phục sau gián đoạn

Crash, mất mạng, tắt máy — **không mất tiến độ**. Chỉ cần chạy lại app trong cùng thư mục, hệ thống tự khôi phục từ điểm cuối cùng đã lưu.

> Muốn bắt đầu tiểu thuyết mới: xóa thư mục `workspace/output/` (hoặc di chuyển sang thư mục khác).

**Quản lý nhiều tiểu thuyết**: Mỗi tiểu thuyết gắn với một thư mục workspace riêng. Dùng volume Docker khác nhau cho từng cuốn:
```bash
-v "$PWD/workspace-truyen-1:/workspace"   # Tiểu thuyết 1
-v "$PWD/workspace-truyen-2:/workspace"   # Tiểu thuyết 2
```

---

## Cấu trúc thư mục output

```
workspace/output/novel/
├── chapters/              # Bản thảo cuối (Markdown)
│   ├── 01.md
│   └── ...
├── drafts/                # Bản nháp chưa hoàn chỉnh
├── reviews/               # Báo cáo đánh giá của Biên tập viên
├── summaries/             # Tóm tắt chương, cung, tập
└── meta/
    ├── premise.md         # Tiền đề câu chuyện
    ├── outline.json       # Đề cương chương
    ├── compass.json       # La bàn hướng kết thúc (truyện dài)
    ├── characters.json    # Hồ sơ nhân vật
    ├── world_rules.json   # Quy tắc thế giới
    ├── foreshadow.json    # Sổ theo dõi phục bút
    ├── progress.json      # Trạng thái tiến độ
    ├── checkpoints.jsonl  # Điểm khôi phục cấp step
    └── diag-export.md     # Báo cáo chẩn đoán ẩn danh (dùng khi báo lỗi)
```

---

## Xuất truyện

```bash
# Trong TUI, gõ:
/export                          # TXT, lưu vào workspace/output/novel/TenTruyen.txt
/export ~/truyen-cua-toi.epub    # EPUB (đọc trên Kindle, Apple Books, v.v.)
/export from=10 to=50            # Xuất chương 10–50
/export from=10 --overwrite      # Ghi đè file cũ
```

**TXT** — Bao gồm toàn bộ nội dung chương, tự động thêm phân cách tập/cung.

**EPUB** — Chuẩn EPUB 3, có mục lục, tương thích hầu hết máy đọc sách. Không có ảnh bìa.

---

## Troubleshooting

### App loop không dừng khi mới khởi động

**Nguyên nhân**: Còn session cũ bị hỏng trong workspace.

**Giải pháp**: Xóa workspace cũ và khởi động lại:
```bash
# Linux/macOS
rm -rf workspace/output/

# Windows PowerShell
Remove-Item -Recurse -Force workspace\output\
```

---

### Coordinator gọi `novel_context` liên tục

**Nguyên nhân phổ biến**:
1. Model quá nhỏ (< 7B) — không đủ năng lực theo protocol phức tạp
2. Session cũ bị hỏng — xóa workspace như trên

**Giải pháp**: Dùng model lớn hơn. Khuyến nghị tối thiểu:
- **Ollama local**: `gemma4:12b` hoặc `qwen3.5:27b`
- **Cloud**: `gemini-2.5-flash`, `claude-sonnet-4`, `gpt-4o`

---

### `[Can thiệp người dùng]` hoặc text tiếng Trung xuất hiện trong TUI

**Nguyên nhân**: Một số file Go chưa được dịch đầy đủ.

**Giải pháp**: Rebuild Docker image sau khi pull phiên bản mới nhất:
```bash
git pull
docker build -t ainovel-cli-vi . --no-cache
```

---

### Lỗi kết nối Ollama từ Docker

```
connection refused / cannot reach localhost
```

**Giải pháp**:
- **Windows/Mac**: Dùng `host.docker.internal` thay `localhost` trong `base_url`
- **Linux**: Dùng IP của bridge Docker: `172.17.0.1` hoặc chạy `ip route | grep docker0`

Kiểm tra Ollama đang chạy:
```bash
ollama list      # Xem models đã cài
ollama ps        # Xem models đang chạy
```

---

### Không tìm thấy file cấu hình

```
Không tìm thấy file cấu hình, bắt đầu thiết lập khởi tạo...
setup: could not open a new TTY
```

**Giải pháp**: Tạo file `config/config.json` thủ công theo hướng dẫn [phần Cấu hình](#cấu-hình) trước khi chạy Docker.

---

## Kiến trúc nội bộ (cho developer)

Xem chi tiết trong thư mục [`docs/`](docs/):
- [`architecture.md`](docs/architecture.md) — Thiết kế tổng thể, nguyên tắc và ba điều luật
- [`context-management.md`](docs/context-management.md) — Pipeline nén ngữ cảnh 4 cấp
- [`observability.md`](docs/observability.md) — Hệ thống quan sát và chẩn đoán
- [`refactor-flow-driven.md`](docs/refactor-flow-driven.md) — Lịch sử tái cấu trúc Flow Router

---

## Công nghệ

- **Go 1.25** — Ngôn ngữ chính
- **[agentcore](https://github.com/voocel/agentcore)** — Nhân Agent (tool-calling + streaming)
- **[Bubble Tea](https://github.com/charmbracelet/bubbletea)** — Framework TUI terminal
- **[litellm](https://github.com/voocel/litellm)** — Adapter LLM thống nhất

---

## Giấy phép

MIT — Dự án gốc: [voocel/ainovel-cli](https://github.com/voocel/ainovel-cli)
