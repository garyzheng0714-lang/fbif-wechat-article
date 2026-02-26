import { Table } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { useNavigate } from 'react-router-dom';
import type { ArticleSummaryItem } from '../../types/wechat';
import { formatNumber } from '../../utils/format';

interface ArticleTableProps {
  data: ArticleSummaryItem[];
  loading?: boolean;
}

export default function ArticleTable({ data, loading }: ArticleTableProps) {
  const navigate = useNavigate();

  const columns: ColumnsType<ArticleSummaryItem> = [
    {
      title: '标题',
      dataIndex: 'title',
      key: 'title',
      ellipsis: true,
      width: '35%',
      render: (title: string, record) => (
        <a
          onClick={() => navigate(`/article/${encodeURIComponent(record.msgid)}`)}
          style={{ cursor: 'pointer' }}
        >
          {title}
        </a>
      ),
    },
    {
      title: '日期',
      dataIndex: 'ref_date',
      key: 'ref_date',
      width: 120,
      sorter: (a, b) => a.ref_date.localeCompare(b.ref_date),
    },
    {
      title: '阅读数',
      dataIndex: 'int_page_read_count',
      key: 'int_page_read_count',
      width: 100,
      sorter: (a, b) => a.int_page_read_count - b.int_page_read_count,
      defaultSortOrder: 'descend',
      render: (v: number) => (
        <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--jade-200)' }}>
          {formatNumber(v)}
        </span>
      ),
    },
    {
      title: '阅读人数',
      dataIndex: 'int_page_read_user',
      key: 'int_page_read_user',
      width: 100,
      sorter: (a, b) => a.int_page_read_user - b.int_page_read_user,
      render: (v: number) => (
        <span style={{ fontFamily: 'var(--font-mono)' }}>{formatNumber(v)}</span>
      ),
    },
    {
      title: '分享',
      dataIndex: 'share_count',
      key: 'share_count',
      width: 80,
      sorter: (a, b) => a.share_count - b.share_count,
      render: (v: number) => (
        <span style={{ fontFamily: 'var(--font-mono)' }}>{formatNumber(v)}</span>
      ),
    },
    {
      title: '收藏',
      dataIndex: 'add_to_fav_count',
      key: 'add_to_fav_count',
      width: 80,
      sorter: (a, b) => a.add_to_fav_count - b.add_to_fav_count,
      render: (v: number) => (
        <span style={{ fontFamily: 'var(--font-mono)' }}>{formatNumber(v)}</span>
      ),
    },
  ];

  return (
    <div className="glass-card dark-table animate-in animate-in-7" style={{ overflow: 'hidden', borderRadius: 'var(--radius-lg)' }}>
      <Table
        columns={columns}
        dataSource={data}
        rowKey="msgid"
        loading={loading}
        pagination={{
          pageSize: 10,
          showSizeChanger: true,
          showTotal: (t) => (
            <span style={{ color: 'var(--text-tertiary)', fontFamily: 'var(--font-mono)', fontSize: 12 }}>
              {t} 篇文章
            </span>
          ),
        }}
        size="middle"
      />
    </div>
  );
}
