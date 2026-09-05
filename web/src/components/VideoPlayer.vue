<template>
  <div class="video-player">
    <video
      ref="videoRef"
      class="video-element"
      :src="videoSrc"
      controls
      @timeupdate="onTimeUpdate"
      @loadedmetadata="onLoadedMetadata"
    />
    <div v-if="showTimestamp" class="timestamp-overlay">
      {{ formatTime(currentTime) }} / {{ formatTime(duration) }}
    </div>
  </div>
</template>

<script setup>
import { ref, watch, onMounted } from 'vue'

const props = defineProps({
  videoSrc: {
    type: String,
    required: true
  },
  startTime: {
    type: Number,
    default: 0
  },
  showTimestamp: {
    type: Boolean,
    default: true
  }
})

const emit = defineEmits(['timeupdate', 'loaded'])

const videoRef = ref(null)
const currentTime = ref(0)
const duration = ref(0)

// 跳转到指定时间
function seekTo(seconds) {
  if (videoRef.value) {
    videoRef.value.currentTime = seconds
    videoRef.value.play()
  }
}

function onTimeUpdate() {
  if (videoRef.value) {
    currentTime.value = videoRef.value.currentTime
    emit('timeupdate', currentTime.value)
  }
}

function onLoadedMetadata() {
  if (videoRef.value) {
    duration.value = videoRef.value.duration
    emit('loaded', duration.value)
    // 如果有起始时间，跳转过去
    if (props.startTime > 0) {
      seekTo(props.startTime)
    }
  }
}

function formatTime(seconds) {
  if (!seconds || isNaN(seconds)) return '00:00'
  const mins = Math.floor(seconds / 60)
  const secs = Math.floor(seconds % 60)
  return `${String(mins).padStart(2, '0')}:${String(secs).padStart(2, '0')}`
}

// 监听 startTime 变化
watch(() => props.startTime, (newTime) => {
  if (newTime > 0 && videoRef.value) {
    seekTo(newTime)
  }
})

// 监听视频源变化
watch(() => props.videoSrc, () => {
  currentTime.value = 0
  duration.value = 0
})

onMounted(() => {
  if (props.startTime > 0 && videoRef.value) {
    videoRef.value.addEventListener('loadedmetadata', () => {
      seekTo(props.startTime)
    }, { once: true })
  }
})

defineExpose({
  seekTo,
  videoRef,
  currentTime,
  duration
})
</script>

<style scoped>
.video-player {
  position: relative;
  width: 100%;
  background: #000;
  border-radius: 8px;
  overflow: hidden;
}

.video-element {
  width: 100%;
  display: block;
  max-height: 500px;
  object-fit: contain;
}

.timestamp-overlay {
  position: absolute;
  top: 12px;
  right: 12px;
  background: rgba(0, 0, 0, 0.6);
  color: #fff;
  padding: 4px 10px;
  border-radius: 4px;
  font-size: 13px;
  font-family: monospace;
}
</style>
