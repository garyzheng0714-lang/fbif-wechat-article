import { Pie } from '@ant-design/charts';
import type { UserShareItem } from '../../types/wechat';
import { SHARE_SCENE_LABELS } from '../../utils/constants';

interface ShareSceneChartProps {
  data: UserShareItem[];
}

export default function ShareSceneChart({ data }: ShareSceneChartProps) {
  const byScene = new Map<number, number>();
  for (const d of data) {
    byScene.set(
      d.share_scene,
      (byScene.get(d.share_scene) || 0) + d.share_count,
    );
  }

  const chartData = Array.from(byScene.entries())
    .map(([scene, count]) => ({
      scene: SHARE_SCENE_LABELS[scene] || `场景${scene}`,
      count,
    }))
    .sort((a, b) => b.count - a.count);

  const config = {
    data: chartData,
    angleField: 'count',
    colorField: 'scene',
    radius: 0.8,
    innerRadius: 0.55,
    theme: 'classicDark',
    scale: { color: { range: ['#58a6ff', '#07c160', '#f0b429'] } },
    label: {
      text: 'scene',
      position: 'outside' as const,
      fill: 'rgba(230,237,243,0.5)',
      fontSize: 11,
    },
    legend: { position: 'bottom' as const, itemLabelFill: 'rgba(230,237,243,0.6)' },
    interaction: { tooltip: true },
  };

  return (
    <div className="glass-card chart-card animate-in animate-in-6">
      <div className="chart-title">分享场景分布</div>
      <Pie {...config} height={280} />
    </div>
  );
}
