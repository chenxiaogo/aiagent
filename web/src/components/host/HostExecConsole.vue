<template>
  <div class="host-exec">
    <div class="exec-bar">
      <el-input
        v-model="command"
        placeholder="输入要执行的命令，如 lscpu / top -bn1 / df -h"
        size="small"
        :disabled="!connected"
        @keyup.ctrl.enter="runCommand"
      >
        <template #prepend>
          <span class="prompt">$</span>
        </template>
      </el-input>
      <el-input-number v-model="timeout" :min="1000" :max="600000" :step="5000" size="small"
        controls-position="right" style="width: 150px; margin-left: 8px" title="超时(ms)" />
      <el-button type="primary" size="small" :disabled="!connected || running" @click="runCommand">运行</el-button>
      <el-button size="small" :disabled="!connected || running" @click="clearOutput">清空</el-button>
    </div>
    <div ref="outEl" class="exec-output" v-loading="running && !output">
      <div v-for="(line, i) in lines" :key="i" :class="['exec-line', line.cls]">{{ line.text }}</div>
      <div v-if="!connected" class="exec-line err">终端连接未建立</div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { ElMessage } from 'element-plus'

const props = defineProps({
  hostId: { type: [Number, String], required: true },
})

const command = ref('')
const timeout = ref(60000)
const connected = ref(false)
const running = ref(false)
const output = ref('')
const lines = ref([])
const outEl = ref(null)

let ws = null

function push(cls, text) {
  lines.value.push({ cls, text })
  nextTick(() => {
    if (outEl.value) outEl.value.scrollTop = outEl.value.scrollHeight
  })
}

function buildWsUrl() {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const token = localStorage.getItem('aiagent_token') || ''
  return `${proto}//${window.location.host}/api/hosts/${props.hostId}/exec?token=${encodeURIComponent(token)}`
}

function connect() {
  ws = new WebSocket(buildWsUrl())
  ws.onopen = () => { connected.value = true }
  ws.onmessage = (ev) => {
    let msg
    try { msg = JSON.parse(ev.data) } catch { return }
    if (msg.type === 'output') {
      push(msg.stream === 'stderr' ? 'err' : 'out', msg.data)
    } else if (msg.type === 'result') {
      running.value = false
      const code = msg.exitCode ?? -1
      push(code === 0 ? 'ok' : 'err', `\n[完成] 退出码=${code} 耗时=${msg.durationMs}ms 状态=${msg.status}\n`)
    } else if (msg.type === 'error') {
      running.value = false
      push('err', `[错误] ${msg.data}\n`)
    } else if (msg.type === 'pong') {
      // ignore
    }
  }
  ws.onclose = () => { connected.value = false; running.value = false }
  ws.onerror = () => { connected.value = false; running.value = false; push('err', '[连接失败]\n') }
}

function runCommand() {
  const cmd = command.value.trim()
  if (!cmd) return ElMessage.warning('请输入命令')
  if (!ws || ws.readyState !== WebSocket.OPEN) return ElMessage.error('终端未连接')
  running.value = true
  push('cmd', `$ ${cmd}`)
  ws.send(JSON.stringify({ type: 'exec', command: cmd, timeout: timeout.value }))
}

function clearOutput() {
  lines.value = []
}

onMounted(connect)
onBeforeUnmount(() => { if (ws) ws.close() })

defineExpose({ connect, runCommand })
</script>

<style scoped>
.host-exec { display: flex; flex-direction: column; height: 100%; }
.exec-bar { display: flex; align-items: center; gap: 4px; margin-bottom: 8px; }
.exec-bar .prompt { font-family: monospace; color: var(--text-muted); }
.exec-output {
  flex: 1; min-height: 320px; max-height: 60vh; overflow: auto;
  background: #1e1e1e; color: #d4d4d4; border-radius: 6px; padding: 10px;
  font-family: Menlo, Consolas, "Courier New", monospace; font-size: 12.5px; line-height: 1.5;
  white-space: pre-wrap; word-break: break-all;
}
.exec-line { margin: 0; }
.exec-line.cmd { color: #6a9955; }
.exec-line.out { color: #d4d4d4; }
.exec-line.err { color: #f48771; }
.exec-line.ok { color: #6a9955; }
</style>
