# WeKnora 当前主线运行基线报告

> 仓库：`D:\WeKnora`；分支：`main`；Commit：`7b2c9e73bf6950edde8165e4fe352714b7e5c16e`  
> 执行日期：2026-07-16（Asia/Shanghai）  
> 最终状态：**FAIL**（基础服务和当前 HEAD 解析/分块已验证；缺少 Embedding/Chat 模型，Chunk 未持久化且知识状态停留在 processing，无法完成检索、回答和 Langfuse Trace）。

## 1. 环境与版本

| 项目 | 实际值 | 状态 |
|---|---|---|
| OS | Windows NT `10.0.26200.0` | 已验证 |
| Shell | Windows PowerShell `5.1.26100.8655` | 已验证 |
| Git | `2.53.0.windows.2` | 已验证 |
| Docker | Client/Server `29.4.2` | 已验证 |
| Docker Compose | `v5.1.3` | 已验证 |
| Docker 资源 | 12 CPU；16,453,419,008 bytes memory | 已验证 |
| Go | `go1.26.1 windows/amd64` | 已验证 |
| Node.js / npm / pnpm | `v24.14.1` / `11.11.0` / `10.33.0` | 已验证 |
| 前端包管理器 | `npm`（存在 `frontend/package-lock.json`，CI/脚本交叉确认） | 已验证 |
| Make | PATH 中不存在 GNU Make | 已验证 |
| Bash/GCC | MSYS2 Bash `5.3.15`；GCC/G++ `16.1`，需为测试临时加入 PATH | 已验证 |
| Python | `3.13.14` | 已验证 |
| D: 可用空间 | 约 90.15 GB（检查时） | 已验证 |
| 关键端口 | 80/8080 由本轮 Compose 使用；5432/6379/50051 仅容器网络；3000、11434 未监听 | 已验证 |
| `.env` | 从 `.env.example` 复制；已确认 `.*` 被 `.gitignore` 排除；未覆盖既有文件、未加入真实密钥 | 已验证 |

工作区开始时干净。当前仅新增本报告和代码库地图；`frontend/node_modules` 与 `.env` 均为 ignored 本地文件。

## 2. 官方启动依据与命令记录

调查顺序为 README/README_CN、开发文档、`Makefile`、`scripts/`、`docker-compose.yml`、CI 和测试脚本。没有自行发明业务启动流程。

### 2.1 默认 Compose

命令：

```powershell
Copy-Item .env.example .env
docker compose up -d
docker compose ps
```

来源：`README.md`、`README_CN.md`、`docker-compose.yml`。  
执行目的：启动官方默认 app、frontend、docreader、postgres、redis。  
预期结果：Web/API 可访问，带健康检查的容器 healthy。  
可能产生的状态变化：创建 `.env`（ignored）、镜像、容器、网络、卷、端口和数据库数据。  
失败后的停止条件：持续无输出时检查 BuildKit/容器状态与日志，而非空等；不修改业务代码。

结果：五个服务均运行；app、docreader、postgres healthy；`http://localhost/` 和 `http://localhost:8080/health` 返回 200。

### 2.2 当前 HEAD 镜像重建与环境修复

默认 `latest` app/ui/docreader 镜像标签显示 revision `974ca359e56cab11f4603d215ac38d28552dbd27`，不是当前 HEAD，因此按仓库官方脚本重建关键后端镜像。

```powershell
$env:PATH='C:\msys64\mingw64\bin;C:\msys64\usr\bin;' + $env:PATH
C:\msys64\usr\bin\bash.exe ./scripts/build_images.sh --app
C:\msys64\usr\bin\bash.exe ./scripts/build_images.sh --docreader
docker compose up -d --force-recreate --no-deps app
docker compose up -d --force-recreate --no-deps docreader
```

来源：`scripts/build_images.sh`。  
执行目的：使运行 app/docreader 与所调查 Commit 一致。  
状态变化：构建 Docker image/cache 并重建两个容器；不改源码。  
实际结果：

- app 第一次构建因从 Go proxy 下载 `lz4` 出现 `unexpected EOF`，重试成功；脚本输出 Commit ID `7b2c9e73`。镜像 `sha256:a8dd9dda1381aabd71303baf9cc8183619c24943430608cdd6606af9090f0fa3`。
- DocReader 第一次在 Debian 12 LibreOffice 依赖下载阶段无法连接 `deb.debian.org`；持续检查 BuildKit 状态后重试成功。镜像 `sha256:d44b3c2817df28216358b8ad1d0d8d7e0d7a9ebb666577a952d4be9ef2ffa838`。
- 用户建议的 `/ubuntu` APT 镜像不适用于该 Debian 12 Dockerfile；重试已成功，故未修改 Dockerfile/Compose。
- 资源为 12 CPU/约 16 GB，排除资源不足为本次根因。
- 两个新容器均 healthy。frontend 仍为 revision `974ca...` 的静态镜像，因此前端运行物未证明与 HEAD 相同；本地 HEAD 前端测试另行执行。

## 3. 服务与健康检查

| 服务 | 容器 | 地址/端口 | 健康检查与结果 | 人工操作 |
|---|---|---|---|---|
| Frontend | `WeKnora-frontend` | `http://localhost/` | HTTP 200 | 可浏览器验证 |
| Go app（当前 HEAD） | `WeKnora-app` | `http://localhost:8080` | `/health` 200，healthy | 无 |
| DocReader（当前工作树构建） | `WeKnora-docreader` | 容器网 `50051` | Compose healthy；app gRPC 调用成功 | 无 |
| PostgreSQL/ParadeDB | `WeKnora-postgres` | 容器网 `5432` | healthy | 无 |
| Redis | `WeKnora-redis` | 容器网 `6379` | running，app/worker 可连接 | 无 |
| MinIO | 当前默认 Compose 未启动 | — | 未验证 | 依部署配置 |
| Neo4j | 当前默认 Compose 未启动 | — | 未验证 | 依 profile/配置 |
| Langfuse | profile 未启动 | 3000 未监听 | BLOCKED | 需项目配置/凭据 |

## 4. 测试文档

| 字段 | 值 |
|---|---|
| 文件 | `docs/api/knowledge-base.md` |
| 类型/大小 | Markdown；30,919 bytes |
| SHA-256 | `15D901F6151377FB6C1C7C17442342136B90C2C1CEFA6171F0429855E5D614B8` |
| 来源 | 当前仓库官方 API 文档 |
| 摘要 | WeKnora 知识库 API、参数和调用示例，内容稳定且可公开核验 |
| 仓库自带/本轮创建 | 仓库自带；本轮未创建文档内容 |

另一次初始验证使用 `README_CN.md`，SHA-256 `B2D5D4E39A597229DC5A51A694A755493B861D1D6494FA4D433C73C49BA05918`。

## 5. 文档上传与解析

### 5.1 当前 HEAD 实际请求

- 使用随机本地测试账号注册/登录，创建测试知识库并通过真实上传 API 上传官方文档；HTTP 200。
- Knowledge Base ID：`6f95696f-3b48-493e-8921-7cac38919155`。
- Document ID：`6f1c9dba-bb43-4b39-8742-7bc64f7502fc`。
- 本地测试 tenant：`10004`（从当前 HEAD app 处理日志交叉确认）。
- 权限：上传要求已认证 tenant/user 和知识库访问权。

### 5.2 解析、Chunking、Embedding 与索引结果

当前 HEAD app 日志已实际观察到：

1. DocReader gRPC 调用成功，文档内容返回。
2. 图片解析为 0 张。
3. auto Chunking 尝试 heading 与 legacy；二者诊断均出现 `chunk exceeds 2x target size` 回退/拒绝信息。
4. 随后明确输出 `Split document into 96 chunks`，证明解析与内存分块已经运行。
5. `processChunks` 调用 `GetEmbeddingModel` 时 model ID 为空，返回 `get embedding model failed`。
6. 三分钟后文档仍为 `processing`；数据库 Chunk 数为 0，未完成 Embedding 和索引；错误未落到文档 `error_message`。

因此当前基线不是“DocReader/Chunking 卡住”，而是**缺少 Embedding 模型后处理失败，且该失败分支未把文档状态正确终结**。源码交叉验证 `internal/types/indexing_strategy.go`：`NeedsEmbedding()` 在 vector 或 keyword 任一启用时均为 true，所以 keyword-only 配置不能绕过模型依赖。

## 6. 当前 Chunking 配置与预览

| 项目 | 基线观察 |
|---|---|
| 策略 | `auto`，来自创建/知识库配置与后端解析 |
| Chunk size / overlap | 源码回退 512/80；迁移/配置另见 overlap 50，当前实例最终配置未通过 UI 截图确认 |
| 父子分块 | 源码/前端可配置；本次实例值未人工确认 |
| 策略回退 | 实际日志观察到 heading/legacy 诊断；最终产生 96 个内存 Chunk |
| Chunk 总数 | 内存分块 96；持久化 0 |
| 预览 | 源码存在预览入口；由于持久化前 Embedding 失败，无法保存三条持久 Chunk 证据 |
| 修改后重解析 | 源码有单文档/批量 reparse；知识库配置保存不会自动证明已重解析 |

不能把内存日志中的 96 个 Chunk 写成“索引完成”。三条代表性持久 Chunk、Chunk ID、向量和预览均为**未验证**。

## 7. 基线问题、检索与回答

预先定义的问题：

1. 直接事实：`创建知识库时必须提供哪些字段？`
2. 跨段组合：`创建知识库后，如何上传文档并查询其处理状态？请结合相关接口说明。`
3. 无答案：`该文档是否说明 WeKnora 在月球部署时需要什么硬件？`

| 验证项 | 状态 | 原因 |
|---|---|---|
| Top-K 检索、Chunk ID、排名、相似度 | BLOCKED | Chunk 未持久化/索引 |
| Rerank 分数 | BLOCKED | 无召回结果且未配置模型 |
| Context/引用 | BLOCKED | 检索链未成立 |
| 三题真实回答 | BLOCKED | 未配置 Chat 模型，禁止编造回答 |
| Faithfulness/Relevance/幻觉人工判断 | 未验证 | 没有真实生成输出 |

## 8. Langfuse Trace

状态：`BLOCKED`  
缺失条件：Langfuse profile/服务、项目 public/secret key、可完成的检索与生成请求。  
当前事实：端口 3000 未监听；源码存在 Trace/Span/Generation，不等于运行 Trace 已验证。处理 span 中可能出现 trace metadata，也不等于已在 Langfuse UI 接收。  
配置步骤：仅按仓库官方 Langfuse 文档/profile 配置服务及密钥；不得将密钥写入报告。  
浏览器验证步骤：登录 Langfuse，按请求时间/Trace ID 查找，展开 retrieval、rerank、generation，核验 input/output/model/token/latency/error/metadata。  
预期证据：Trace 页面截图、脱敏 Trace ID、时间、各 span/generation 字段。  
结论：检索信息、rerank、引用、Score 均未运行验证；源码也未找到 Score writer。

## 9. 已有测试结果

### 9.1 Go 全量测试

命令与来源：`go test -json ./...`，Go 标准/CI 测试方式；需要 CGO、Bash，部分包依赖外部服务。临时把 MSYS2 Bash/GCC 加入当前进程 PATH，未安装系统依赖。

结果（165.91 秒）：

- test pass events：3561；fail：10；skip：5。
- package pass：57；package fail：5。
- DocReader `TestReadURL`/`TestReadFile` 在默认 production Compose 中因 50051 未映射宿主而失败；启动临时 dev DocReader 后定向通过。
- 6 个 Notion connector 测试因 SSRF 对 loopback 的阻断失败。
- 2 个 remote-image 测试在全量中失败、定向重跑通过，存在环境/顺序不稳定性。
- agent/tools 与 container link 相关失败落在 Windows MSYS2/DuckDB ABI/链接环境。

### 9.2 CLI 测试

按仓库命令运行，并为单次命令设置 `GOPROXY=https://goproxy.cn,direct`：30.15 秒，pass 1179、fail 0、skip 1；package pass 33、fail 0。

### 9.3 前端测试

命令：

```powershell
cd frontend
npm ci --no-audit --no-fund
npm test
```

来源：`frontend/package.json`、lockfile/CI。  
状态变化：只创建 ignored 的 `frontend/node_modules`；未改 lockfile、源码或测试。  
结果：384 个依赖安装成功；185 tests 中 184 通过、1 失败，约 2.529 秒。失败：`src/i18n/locales/workspaceTerminology.test.ts` 的 zh-CN `integrations.api.capabilityManageStorageBackendsHint` 仍包含 `租户默认存储设置`。这是稳定的当前代码/文案断言失败，没有修改测试规避。

## 10. 浏览器人工操作清单

以下步骤是为了补齐 CLI 无法证明的 UI/Trace 证据；在模型配置完成前，步骤 6–10 不可判定通过。

| 步骤 | 页面地址与点击位置 | 输入内容 | 预期结果/截图 | 需记录 | 通过标准 |
|---|---|---|---|---|---|
| 1 登录 | `http://localhost/` → 登录 | 本地测试账号；不得截图密码 | `01-login.png`，首页与用户标识 | 登录时间、tenant/user ID | 无认证错误 |
| 2 创建知识库 | 知识库列表 → 新建 | `rag-eval-baseline` | `02-kb-created.png` | KB ID、时间 | 详情页可打开 |
| 3 上传文档 | 文档 → 上传 | `docs/api/knowledge-base.md` | `03-upload.png` | 文档 ID、时间 | 上传响应成功 |
| 4 解析进度 | 文档列表/详情 | 无 | `04-parse-progress.png` | 状态、起止、错误 | 最终 completed；当前未达成 |
| 5 Chunking 配置 | KB 设置 → Chunking | 只查看 | `05-chunk-config.png` | strategy/size/overlap/父子配置 | 与 API/源码一致 |
| 6 Chunk 预览 | 文档详情 → 预览 | 无 | `06-chunk-preview.png` | 总数、3 个 Chunk/ID | 有持久 Chunk；当前阻塞 |
| 7 三个问题 | 问答页 | 本报告第 7 节三题 | `07-q1.png`、`08-q2.png`、`09-q3.png` | Query、时间、session/message ID | 三次真实完成 |
| 8 召回引用 | 每个回答的来源/引用 | 无 | 同上，展开引用 | Top-K、Chunk ID、排名、分数 | 正确证据可核验 |
| 9 回答 | 问答详情 | 无 | 完整回答截图 | 模型、延迟、token、人工判断 | 不伪造、可追溯 |
| 10 Langfuse | Langfuse UI → Traces | 按时间/Trace ID 搜索 | `10-langfuse-trace.png` | Trace ID、span/generation、时间 | UI 实际存在记录 |

## 11. 当前已知问题与阻塞项

1. 必需的 Embedding/Chat 模型未配置；本机没有 Ollama CLI、11434 服务、缓存镜像或容器。原始任务禁止安装系统级依赖，也禁止伪造密钥，因此未擅自安装/下载大型模型。
2. 当前 HEAD 在 Embedding model ID 为空时已经分出 96 个 Chunk，但没有持久化，并把知识长期留在 `processing`，错误信息未正常落库。这是影响基线的实际失败。
3. Langfuse 未配置，不能把源码接口当运行通过。
4. frontend 静态容器不是当前 HEAD；本地 HEAD 单测已有 1 项失败。
5. 全量 Go 测试含 Windows ABI、SSRF loopback 与服务暴露相关失败；没有修改测试。

## 12. 本轮产生的运行数据和本地文件

- ignored：`.env`（只由官方模板复制，无真实密钥）、`frontend/node_modules/`。
- Docker：镜像、BuildKit cache、Compose network/volumes、5 个运行容器；app/docreader 为本轮当前工作树重建。
- PostgreSQL：随机本地测试用户/tenant、多个测试知识库和 3 份上传文档记录；包括上文列出的当前 HEAD KB/document ID。未写入外部生产数据。
- 临时开发 DocReader 容器用于定向 Go 测试，完成后不作为最终服务证据。
- Git：没有 add、commit、checkout、switch、pull、reset 或 clean。

## 13. 最终判定

**FAIL**

基础设施、上传、DocReader 和内存 Chunking 已真实通过；但主链在 Embedding 模型解析处失败，未产生持久 Chunk/索引，文档状态处理也异常，检索、Rerank、回答、引用和 Langfuse 均无法实际验证。依状态定义，这不是可以标成 PASS/PARTIAL 的主要链路成功，也不能用人工回答或源码存在替代运行事实。
