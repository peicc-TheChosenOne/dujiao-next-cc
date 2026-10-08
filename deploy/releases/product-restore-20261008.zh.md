# 商品原位恢复修复（2026-10-08）

生产已上线 `v1.0.0+product-restore.20261008`，代码提交 `47f406f4a985e11c16f67b08d7f3881c5f4837ee`，取代此前“同标识创建新商品”的实现。

## 最终规则

创建请求遇到已删除的同标识商品时，恢复原记录，保留商品 ID 和创建时间，并完整保存本次输入，包括标题、价格、图片、空值、排序和上架状态。在用商品继续返回标识已存在。商品恢复与规格更新使用同一事务，规格更新失败会回滚恢复。

恢复单规格商品时处理旧的软删除默认 SKU，避免 SKU 唯一键冲突；多规格沿用现有同步流程。规格按新输入保存，本次承诺保留的是商品 ID。已删除的卡密、购物车及映射等关系不会自动恢复。

模型恢复全局 Slug 唯一索引 `idx_products_slug`，启动迁移先建立它，再移除前一次发布的部分索引。生产发布前已确认没有同 Slug 的重复记录，没有合并或删除商品数据。发布只包含本次后端修复，工作区其他未提交修改未包含在镜像中。

## 验证

- 修复前测试证明旧实现会在同标识重新创建时生成新商品 ID。
- Linux Go 1.26.5：`go test -p 1 ./internal/modules/catalog/product/... ./internal/bootstrap/database/migrations ./internal/bootstrap/catalogproduct -count=1` 全部通过。
- 独立 PostgreSQL 后台 API 验证升级后单规格、多规格恢复均保留 ID 和创建时间，更新内容及空值正确，重复删除恢复、在用重名拦截、规格失败回滚和重启迁移通过。测试库已删除。
- 生产事务验证 `chatgpt-plus` 原 ID 2 可以恢复，创建时间保留，全局唯一索引拦截额外同名记录。事务回滚，全部商品数据指纹保持一致，没有留下测试数据。
- 公网 HTTPS 首页、健康检查、公共配置和商品列表均 HTTP 200，公共配置返回恢复版版本。应用健康，启动后日志没有 error/fatal/panic；数据库和 Redis 容器、运行配置保持原样。

## 发布与回退记录

- 镜像：`dujiao-next:fix-product-restore-20261008`。
- 镜像 ID：`sha256:f3af475a38714b53f64325636486b127ca3d8901f8834403c030958d56d46803`。
- 旧镜像：`sha256:a5710d138e588abfbbce9320953c7056e22df0c6865ada9ded359a0c8911f3eb`。
- 部署前备份：`/opt/dujiao-next/backups/dujiao-next-20261008T080845Z.tar.gz`，已恢复至临时 PostgreSQL 库验证，共 61 张表。
- 源码、测试、构建日志及证据：`/opt/dujiao-next/releases/product-restore-20261008/`；当前运行记录：`/opt/dujiao-next/deployed.json`。

回退脚本先恢复上一版本的部分唯一索引，再切回旧应用；这会恢复上一版本“同标识创建新 ID”的行为。该兼容过程已在独立 PostgreSQL 库验证，生产仅做只读预检，未回退或恢复旧数据库。

```bash
python3 /opt/dujiao-next/releases/product-restore-20261008/rollback.py --check
python3 /opt/dujiao-next/releases/product-restore-20261008/rollback.py
```

本次没有调整 ESA 网络路径。
