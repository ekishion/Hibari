<script setup lang="ts">
// 文件列表容器：返回上一级行 + 排序后的行 + 空态
const { visibleFiles, parentPath, currentDir, searchQuery } = useFiles()
</script>

<template>
  <div id="file-list">
    <FileRow v-if="parentPath" :parent-path="parentPath" :current-dir="currentDir" />
    <div v-if="!visibleFiles.length" class="empty-state">
      <div class="empty-icon"><AppIcon name="folder-open" :size="24" /></div>
      <p>{{ searchQuery ? "未找到匹配的文件" : "此文件夹为空" }}</p>
    </div>
    <FileRow v-for="file in visibleFiles" :key="file.fs_id ?? file.path ?? fileNameOf(file)" :file="file" :current-dir="currentDir" />
  </div>
</template>
