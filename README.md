# cchp-cli

CCHP（Claude Code Hub Plus）的命令行工具。命令行代码尚未开始编写，仓库目前只收录 CCHP 控制面的 OpenAPI 文档。

## OpenAPI 文档

| 文件 | 说明 |
| --- | --- |
| `openapi/control-plane.yaml` | 控制面 API，OpenAPI 3.1，YAML |
| `openapi/control-plane.json` | 同一份文档的 JSON 版本 |

- 来源：CCHP v1.0.6 导出的控制面接口说明，共 461 个路径。
- 运行中的 CCHP 实例在 `/api/openapi.json` 与 `/api/openapi.yaml` 提供同一份文档，只有管理员可以访问。

## 认证

命令行工具使用管理 API token：在控制台「账户 → 安全 → 管理令牌」（`/app/account/security`）创建，调用时带上请求头

```
Authorization: Bearer cch_auth_live_<prefix>_<secret>
```

- 大多数控制面接口接受管理 token，文档里每个操作的 `security` 字段列出它接受的凭据，管理 token 对应 `managementBearer`。
- 用管理 token 调用时不需要 CSRF token 与同源 `Origin`。
- 登录、注册、找回密码、两步验证与 Passkey 等账户安全接口只接受浏览器会话，命令行工具不使用这些接口。
- 管理员在系统设置里为某个动作开启人机验证后，该动作的接口返回 `428`，响应的 `details.challenge` 写明动作名称。
