# multi-provider-routing

把 yuanbao 从硬编码单一 provider 重构为可插拔 provider 注册机制。
新增 qwen、kimi 占位骨架（未实现，返回 501）。引入 provider 包与
Provider 接口；RateLimiter 从全局单例改为按 provider 持有；RuntimeConfig
升级为多站点表；面板新增站点管理 tab；保持单账号设计不变。