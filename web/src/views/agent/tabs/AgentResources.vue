<template>
  <div class="resource-container">
    <ResourceBinding :agent-id="agentId" />
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import ResourceBinding from '@/components/agent/ResourceBinding.vue'

const route = useRoute()
const agentId = computed(() => Number(route.params.id) || 0)
</script>

<style scoped>
.resource-container { background: var(--card-bg); border: 1px solid var(--border-light); border-radius: var(--radius-lg); padding: 8px 16px 16px; }
</style>
