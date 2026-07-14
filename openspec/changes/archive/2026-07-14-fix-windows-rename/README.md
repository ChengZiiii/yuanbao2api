# fix-windows-rename

修复 `SaveRuntimeConfig` 在 Windows 下因 `os.Rename` 被防病毒/共享冲突
拒绝而失败的问题（错误：Access is denied）。引入可重试的原子 rename 助手。