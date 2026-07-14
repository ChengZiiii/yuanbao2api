# yuanbao-cookie-fields

把面板 Cookie 输入从单个字符串字段拆分为 `hy_token` 与 `hy_user` 两个
独立输入框；运行时数据模型由 `*string` 改为 `*YuanbaoCookie` 结构体；
服务端在请求时自动拼装 Cookie 头；保留对旧字符串格式的反序列化兼容。