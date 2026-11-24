<template>
  <div class="emoji-pop" @click.stop>
    <div class="emoji-tabs">
      <span v-for="(g, i) in groups" :key="g.name" :class="{ on: tab === i }" @click="tab = i">{{ g.name }}</span>
    </div>
    <div class="emoji-grid">
      <span v-for="e in groups[tab].items" :key="e" @click="$emit('pick', e)">{{ e }}</span>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { EMOJI_GROUPS } from './emoji'

defineProps({ groups: { type: Array, default: () => EMOJI_GROUPS } })
defineEmits(['pick'])
const tab = ref(0)
</script>
