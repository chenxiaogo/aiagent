// 流式 delta 缓冲区：使用 requestAnimationFrame 批量合并
// 避免高频 DOM 更新，参考 1Shell 实现
export function createStreamDeltaBuffer(onFlush) {
  let pending = ''
  let frameId = null

  function flush() {
    if (pending.length > 0) {
      onFlush(pending)
      pending = ''
    }
    frameId = null
  }

  return {
    push(delta) {
      pending += delta
      if (frameId === null) {
        frameId = requestAnimationFrame(flush)
      }
    },
    flushNow() {
      if (frameId !== null) {
        cancelAnimationFrame(frameId)
        frameId = null
      }
      flush()
    },
    getPending() {
      return pending
    }
  }
}

// WebSocket 重连管理
export function createWSReconnect(url, options = {}) {
  const {
    maxRetries = 10,
    retryDelay = 2000,
    maxDelay = 30000,
    onOpen = () => {},
    onMessage = () => {},
    onError = () => {},
    onClose = () => {},
    onReconnect = () => {}
  } = options

  let ws = null
  let retries = 0
  let reconnectTimer = null
  let intentionalClose = false

  function connect() {
    if (intentionalClose) return
    ws = new WebSocket(url)

    ws.onopen = () => {
      retries = 0
      onOpen()
    }

    ws.onmessage = (event) => onMessage(event)

    ws.onerror = () => {
      onError()
      ws.close()
    }

    ws.onclose = () => {
      onClose()
      if (!intentionalClose && retries < maxRetries) {
        const delay = Math.min(retryDelay * Math.pow(1.5, retries), maxDelay)
        retries++
        onReconnect(retries, delay)
        reconnectTimer = setTimeout(connect, delay)
      }
    }
  }

  return {
    connect() {
      intentionalClose = false
      connect()
    },
    send(data) {
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(data)
      }
    },
    close() {
      intentionalClose = true
      if (reconnectTimer) clearTimeout(reconnectTimer)
      if (ws) ws.close()
    },
    get readyState() {
      return ws ? ws.readyState : WebSocket.CLOSED
    }
  }
}