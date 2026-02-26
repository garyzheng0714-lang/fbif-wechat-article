import { Pie } from '@ant-design/charts';
import type { UserReadItem } from '../../types/wechat';
import { USER_SOURCE_LABELS } from '../../utils/constants';

interface TrafficSourceChartProps {
  data: UserReadItem[];
}

export default function TrafficSourceChart({ data }: TrafficSourceChartProps) {
  const sourceData = data.filter((d) => d.user_source !== 99999999);

  const bySource = new Map<number, number>();
  for (const d of sourceData) {
    bySource.set(
      d.user_source,
      (bySource.get(d.user_source) || 0) + d.int_page_read_count,
    );
  }

  const chartData = Array.from(bySource.entries())
    .map(([source, count]) => ({
      source: USER_SOURCE_LABELS[source] || `来源${source}`,
      count,
    }))
    .sort((a, b) => b.count - a.count);

  const config = {
    data: chartData,
    angleField: 'count',
    colorField: 'source',
    radius: 0.8,
    innerRadius: 0.55,
    theme: 'classicDark',
    scale: { color: { range: ['#07c160', '#58a6ff', '#f0b429', '#bc8cff', '#f97066', '#13c2c2', '#eb2f96'] } },
    label: {
      text: 'source',
      position: 'outside' as const,
      fill: 'rgba(230,237,243,0.5)',
      fontSize: 11,
    },
    legend: { position: 'bottom' as const, itemLabelFill: 'rgba(230,237,243,0.6)' },
    interaction: { tooltip: true },
  };

  return (
    <div className="glass-card chart-card animate-in animate-in-6">
      <div className="chart-title">流量来源分布</div>
      <Pie {...config} height={280} />
    </div>
  );
}
