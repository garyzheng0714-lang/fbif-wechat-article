export function formatNumber(num: number): string {
  if (num >= 100000) {
    return (num / 10000).toFixed(1) + '万';
  }
  return num.toLocaleString('zh-CN');
}

export function formatTime(date: Date): string {
  return date.toLocaleTimeString('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}
