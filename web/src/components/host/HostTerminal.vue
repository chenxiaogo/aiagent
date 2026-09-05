<template>
  <div class="host-terminal">
    <div ref="termEl" class="term-box"></div>
    <div v-if="closed" class="term-closed">
      <el-icon><CircleClose /></el-icon>
      <span>终端连接已断开</span>
      <el-button link type="primary" @click="connect">重新连接</el-button>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { Terminal } from 'xterm'
import { FitAddon } from 'xterm-addon-fit'
import 'xterm/css/xterm.css'
import { ElMessage } from 'element-plus'

const props = defineProps({
  hostId: { type: [Number, String], required: true },
})

const termEl = ref(null)
const closed = ref(false)

let term = null
let fitAddon = null
let ws = null
let resizeObs = null

function buildWsUrl() {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const token = localStorage.getItem('aiagent_token') || ''
  return `${proto}//${window.location.host}/api/hosts/${props.hostId}/terminal?token=${encodeURIComponent(token)}`
}

function sendResize() {
  if (!ws || ws.readyState !== WebSocket.OPEN || !fitAddon) return
  const { rows, cols } = term
  ws.send(JSON.stringify({ type: 'resize', size: { rows, cols, width: 0, height: 0 } }))
}

function connect() {
  closed.value = false
  term.reset()
  ws = new WebSocket(buildWsUrl())

  ws.onopen = () => {
    nextTick(sendResize)
  }

  ws.onmessage = (ev) => {
    let msg
    try {
      msg = JSON.parse(ev.data)
    } catch {
      term.write(ev.data)
      return
    }
    if (msg.type === 'output' && typeof msg.data === 'string') {
      term.write(msg.data)
    } else if (msg.type === 'error') {
      term.write('\r\n\x1b[31m' + (msg.data || '错误') + '\x1b[0m\r\n')
    } else if (msg.type === 'close') {
      closed.value = true
      term.write('\r\n\x1b[33m[连接已关闭]\x1b[0m\r\n')
    }
  }

  ws.onclose = () => {
    closed.value = true
  }

  ws.onerror = () => {
    term.write('\r\n\x1b[31m[终端连接失败]\x1b[0m\r\n')
    closed.value = true
  }

  term.onData((data) => {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'input', data }))
    }
  })
}

function dispose() {
  if (ws) {
    try { ws.close() } catch { /* ignore */ }
    ws = null
  }
  if (resizeObs) {
    resizeObs.disconnect()
    resizeObs = null
  }
  if (term) {
    term.dispose()
    term = null
  }
}

onMounted(() => {
  term = new Terminal({
    fontSize: 13,
    fontFamily: 'Menlo, Consolas, "Courier New", monospace',
    cursorBlink: true,
    theme: { background: '#1e1e1e' },
  })
  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.open(termEl.value)
  nextTick(() => {
    fitAddon.fit()
    connect()
    resizeObs = new ResizeObserver(() => {
      try { fitAddon.fit() } catch { /* ignore */ }
      sendResize()
    })
    resizeObs.observe(termEl.value)
  })
})

onBeforeUnmount(dispose)

defineExpose({ connect, dispose })
</script>

<style scoped>
.host-terminal { position: relative; height: 100%; display: flex; flex-direction: column; }
.term-box { flex: 1; min-height: 360px; background: #1e1e1e; border-radius: 6px; padding: 8px; }
.term-closed {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center; gap: 8px;
  background: rgba(30, 30, 30, 0.85); color: #e6a23c; font-size: 13px;
}
</style>
