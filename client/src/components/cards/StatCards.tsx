import { Row, Col } from 'antd';
import {
  EyeOutlined,
  UserOutlined,
  ShareAltOutlined,
  StarOutlined,
} from '@ant-design/icons';
import type { UserReadItem } from '../../types/wechat';
import { formatNumber } from '../../utils/format';

interface StatCardsProps {
  userReadData: UserReadItem[];
}

export default function StatCards({ userReadData }: StatCardsProps) {
  const allSourceData = userReadData.filter((d) => d.user_source === 99999999);

  const totalReads = allSourceData.reduce(
    (sum, d) => sum + d.int_page_read_count,
    0,
  );
  const totalReaders = allSourceData.reduce(
    (sum, d) => sum + d.int_page_read_user,
    0,
  );
  const totalShares = allSourceData.reduce((sum, d) => sum + d.share_count, 0);
  const totalFavs = allSourceData.reduce(
    (sum, d) => sum + d.add_to_fav_count,
    0,
  );

  const cards = [
    { label: '阅读量', value: totalReads, icon: <EyeOutlined />, theme: 'jade' },
    { label: '阅读人数', value: totalReaders, icon: <UserOutlined />, theme: 'sky' },
    { label: '分享次数', value: totalShares, icon: <ShareAltOutlined />, theme: 'amber' },
    { label: '收藏次数', value: totalFavs, icon: <StarOutlined />, theme: 'coral' },
  ];

  return (
    <Row gutter={[16, 16]}>
      {cards.map((card, i) => (
        <Col xs={12} sm={12} md={6} key={card.label}>
          <div className={`glass-card stat-card stat-card--${card.theme} animate-in animate-in-${i + 1}`}>
            <div className="stat-icon">{card.icon}</div>
            <div className="stat-label">{card.label}</div>
            <div className="stat-value">{formatNumber(card.value)}</div>
          </div>
        </Col>
      ))}
    </Row>
  );
}
