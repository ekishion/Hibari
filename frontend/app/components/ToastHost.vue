<script setup lang="ts">
const { state } = useToast()
</script>

<template>
  <Teleport to="body">
    <div id="toast" class="toast" :class="[`toast-${state.type}`, { 'is-visible': state.visible }]" role="status" aria-live="polite">
      <span class="toast-icon">
        <AppIcon :name="state.type === 'error' ? 'circle-alert' : state.type === 'warning' ? 'triangle-alert' : state.type === 'success' ? 'circle-check' : 'info'" :size="18" />
      </span>
      <span class="toast-text">{{ state.message }}</span>
    </div>
  </Teleport>
</template>
