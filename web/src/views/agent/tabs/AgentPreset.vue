<template>
  <PresetPane :agent-id="agentId" />
</template>

<script setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import PresetPane from '@/components/agent/PresetPane.vue'

const route = useRoute()
const agentId = computed(() => Number(route.params.id) || 0)
</script>
