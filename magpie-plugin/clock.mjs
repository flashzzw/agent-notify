// 可替换的时钟与定时器：网关钩子和分发中枢共用一份，测试里整体换成假的。
let nowFn = () => Date.now()
let laterFn = (fn, ms) => { const t = setTimeout(fn, ms); t.unref?.(); return t }
let cancelFn = (t) => clearTimeout(t)

export const now = () => nowFn()
export const later = (fn, ms) => laterFn(fn, ms)
export const cancel = (t) => cancelFn(t)

// 测试用：钉住时钟和定时器；不传参数恢复真实实现。
export function setClock(clock, timers) {
  nowFn = clock ?? (() => Date.now())
  laterFn = timers?.later ?? ((fn, ms) => { const t = setTimeout(fn, ms); t.unref?.(); return t })
  cancelFn = timers?.cancel ?? ((t) => clearTimeout(t))
}
