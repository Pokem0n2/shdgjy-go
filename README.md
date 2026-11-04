# shdgjy-go

进出场身份验证系统（数据接口 + 前端页面）。数据接口是 **Go 单二进制**（约 176 行，仅标准库，无任何第三方依赖），打包为 Docker 镜像运行；前端是单文件 `shdgjy.html`，放在现有 nginx 静态站点即可。**不装 PHP、不改 nginx 配置、不影响服务器上已有的任何站点和容器。**

## 文件说明

| 文件 | 作用 |
|---|---|
| `api.go` | 数据接口源码。GET 返回加密数据或 `nodata`；POST 校验写入凭证后保存数据（原子写，写一半断电不会损坏文件）。所有数据经页面端加密后存储，服务器上只有密文 |
| `Dockerfile` | 把编译好的二进制装入 alpine:3.20（最终镜像约 18MB） |
| `go.mod` | Go module 定义（仅标准库，无依赖） |
| `shdgjy.html` | 前端页面（单文件，含纯 JS 加密实现 + 二维码核验），部署后浏览器直接访问使用 |

## 一、源码如何打包成 tar（本地有 Go 和 Docker 的机器上执行）

### 1. 交叉编译二进制

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o shdgjy-api-amd64 api.go
```

参数说明：
- `CGO_ENABLED=0`：纯静态编译，不依赖目标机器的任何系统库
- `GOOS=linux GOARCH=amd64`：目标平台为 Linux x86_64（阿里云 ECS 绝大多数为此架构；ARM 服务器改为 `GOARCH=arm64`）
- `-ldflags="-s -w"`：去掉符号表和调试信息，二进制更小
- `-o shdgjy-api-amd64`：输出文件名，与 Dockerfile 中 COPY 的名字对应

### 2. 构建镜像并导出为 tar

```bash
docker build -t shdgjy-api:latest .
docker save -o shdgjy-api-image.tar shdgjy-api:latest
```

- `docker build -t shdgjy-api:latest .`：用当前目录的 Dockerfile 构建镜像，命名为 `shdgjy-api`，标签 `latest`
- `docker save -o <文件名> <镜像名>`：把镜像导出为单个 tar 文件，供上传到服务器后 `docker load` 导入（离线安装，服务器不需要访问 Docker Hub）

产物 `shdgjy-api-image.tar`（约 18MB）即部署包。

## 二、服务器部署（宝塔终端或 SSH，共三行命令）

先把 `shdgjy-api-image.tar` 上传到服务器任意位置（如 `/root/`），然后：

```bash
docker load < /root/shdgjy-api-image.tar
```
> 把 tar 中的镜像导入本机 Docker。`<` 表示从文件读入（等价写法 `docker load -i 文件名`）。导入后可用 `docker images` 看到 `shdgjy-api:latest`。加载完成后 tar 文件可删除。

```bash
docker run -d --name shdgjy-api --restart always -p 8443:8443 -v /www/shdgjy:/data shdgjy-api:latest
```
> 从镜像启动容器。参数逐个说明：
> - `-d`：后台运行
> - `--name shdgjy-api`：容器命名（便于后续 `docker logs shdgjy-api` 查看日志、`docker restart shdgjy-api` 重启）
> - `--restart always`：开机自启 + 崩溃后自动重启（生产环境必须）
> - `-p 8443:8443`：端口映射「宿主机端口:容器端口」。接口固定监听 8443，对外也是 8443
> - `-v /www/shdgjy:/data`：目录映射「宿主机路径:容器内路径」。加密数据文件实际落盘在宿主机 `/www/shdgjy/shdgjy-data.json`——容器删了重建，数据不丢
> - `shdgjy-api:latest`：使用的镜像

```bash
curl http://127.0.0.1:8443/
```
> 本机自检。返回 `{"code":"nodata","ok":false}` 即部署成功——这个 `ok:false` 是正常应答，表示"接口正常，服务器上还没有数据"，首次在页面里点"保存"后即变为 `{"ok":true,"data":...}`。

**还需一步**：阿里云控制台 → 安全组 → 入方向规则 → 放行 TCP 8443（页面通过 `域名:8443` 访问此接口）。

## 三、shdgjy.html 的上传位置与使用方法

### 上传位置

宝塔面板 → 文件 → 进入 `/www/server/nginx/html/` → 上传 `shdgjy.html`。与站点里其他 html 完全一样，无需任何 nginx 配置改动。

### 使用方法

浏览器访问 `http://你的域名/shdgjy.html`（如 `http://yzz.cool/shdgjy.html`）：

- **管理员**：输入管理员用户名和密码登录 → 用户管理页（添加/编辑/删除用户、批量导入、导出 CSV、修改统一密码、每用户一个二维码）→ 点「保存」写入服务器 → 之后所有修改即时生效，**无需再上传 html 或重启容器**
- **普通用户**：输入用户名和统一密码 → 显示「核验已通过 ✅」及姓名、车牌号，下方居中展示本人二维码（点击放大、长按可保存图片）
- **门卫保安**：微信扫描用户二维码 → 直接显示该用户「核验已通过 ✅」姓名、车牌号；无效码/已删用户显示核验失败

### 首次部署注意

1. 立即用初始管理员密码登录 → 「密码设置」中**修改管理员密码和统一密码**（初始密码由构建方在生成 html 时指定，仓库中不含明文）
2. 页面顶部若显示"服务器暂无数据"属正常（`nodata` 状态），首次保存后消失
3. 若页面提示离线/连接不上：检查容器是否在跑（`docker ps | grep shdgjy`）和安全组 8443 是否放行

## 二维码说明

- 一人一码：票据用数据信封中的一次性随机密钥加密（AES-256-CTR+HMAC），自包含用户名/姓名/车牌
- **改密码不影响已分发的二维码**；编辑用户信息后旧码显示旧信息（管理页会提示重新分发）；删除用户后旧码立即失效
- 管理页用户表格「二维码」列点击可放大；普通用户登录页可自查并保存自己的码
- 升级到含二维码的版本后，管理员首次登录会提示"二维码功能已启用"，点一次「保存」即生效

## 数据与安全

- 用户数据（姓名、车牌号）在浏览器端用 AES-256-CTR 加密后才上传，服务器磁盘上只有密文（`/www/shdgjy/shdgjy-data.json`）
- 写入接口需凭证（管理员密码派生的 SHA-256），不知管理员密码无法篡改数据
- 接口二进制无脚本执行能力、无数据库、无外部依赖，攻击面极小
- 备份：直接复制 `/www/shdgjy/shdgjy-data.json` 即可
