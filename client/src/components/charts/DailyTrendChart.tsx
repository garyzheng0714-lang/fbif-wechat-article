import { Line } from '@ant-design/charts';
import type { UserReadItem } from '../../types/wechat';

interface DailyTrendChartProps {
  data: UserReadItem[];
}

export default function DailyTrendChart({ data }: DailyTrendChartProps) {
  const allSource = data.filter((d) => d.user_source === 99999999);

  const chartData = allSource.flatMap((d) => [
    { date: d.ref_date, value: d.int_page_read_count, type: '阅读量' },
    { date: d.ref_date, value: d.share_count, type: '分享数' },
  ]);

  const config = {
    data: chartData,
    xField: 'date',
    yField: 'value',
    colorField: 'type',
    smooth: true,
    theme: 'classicDark',
    scale: { color: { range: ['#07c160', '#58a6ff'] } },
    axis: {
      y: { title: '', labelFill: 'rgba(230,237,243,0.4)', gridStroke: 'rgba(255,255,255,0.04)' },
      x: { title: '', labelFill: 'rgba(230,237,243,0.4)' },
    },
    legend: { position: 'top' as const, itemLabelFill: 'rgba(230,237,243,0.6)' },
    interaction: { tooltip: { marker: true } },
    style: { lineWidth: 2.5 },
    area: { style: { fillOpacity: 0.08 } },
  };

  return (
    <div className="glass-card chart-card animate-in animate-in-5">
      <div className="chart-title">每日阅读 / 分享趋势</div>
      <Line {...config} height={280} />
    </div>
  );
}
