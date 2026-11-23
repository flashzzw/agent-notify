// 宿主入口：magpie 加载本包并调用 server() 时，启动接收 hook 上报的本地服务。
// 这个文件只能有 default 导出——宿主把每个导出的函数都当作一个插件。
import { ensureService } from "./service.mjs"

export default {
  id: "agent-notify",
  server: async (_input, options) => {
    await ensureService(options)
    return {}
  },
}
