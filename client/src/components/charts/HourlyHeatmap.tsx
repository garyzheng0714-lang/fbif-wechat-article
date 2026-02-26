import { Heatmap } from '@ant-design/charts';
import type { UserReadHourItem } from '../../types/wechat';

interface HourlyHeatmapProps {
  data: UserReadHourItem[];
}

export default function HourlyHeatmapChart({ data }: HourlyHeatmapProps) {
  const allSource = data.filter((d) => d.user_source === 99999999);

  const chartData = allSource.map((d) => ({
    hour: String(d.ref_hour).padStart(2, '0') + ':00',
    date: d.ref_date,
    reads: d.int_page_read_count,
  }));

  const config = {
    data: chartData,
    xField: 'hour',
    yField: 'date',
    colorField: 'reads',
    theme: 'classicDark',
    legend: { position: 'bottom' as const, itemLabelFill: 'rgba(230,237,243,0.6)' },
    mark: 'cell' as const,
    style: { inset: 1 },
    scale: {
      color: {
        range: ['#0d1117', '#064e3b', '#059669', '#07c160', '#6ee7a8'],
      },
    },
    axis: {
      x: { labelFill: 'rgba(230,237,243,0.4)' },
      y: { labelFill: 'rgba(230,237,243,0.4)' },
    },
    interaction: { tooltip: true },
  };

  return (
    <div className="glass-card chart-card animate-in animate-in-7">
      <div className="chart-title">每小时阅读热力图</div>
      <Heatmap {...config} height={280} />
    </div>
  );
}
