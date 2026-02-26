import { useState, useEffect } from 'react';
import { Form, Input, Button, Alert, Space } from 'antd';
import {
  KeyOutlined,
  AppstoreOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
} from '@ant-design/icons';
import { api } from '../api/wechat';

interface ConfigStatus {
  configured: boolean;
  appid: string;
}

export default function Settings() {
  const [form] = Form.useForm();
  const [status, setStatus] = useState<ConfigStatus | null>(null);
  const [saving, setSaving] = useState(false);
  const [result, setResult] = useState<{ type: 'success' | 'error'; message: string } | null>(null);

  useEffect(() => {
    api.getConfigStatus().then(setStatus).catch(() => {});
  }, []);

  const handleSave = async (values: { appid: string; secret: string }) => {
    setSaving(true);
    setResult(null);
    try {
      const res = await api.saveCredentials(values.appid, values.secret);
      setStatus({ configured: true, appid: res.appid });
      setResult({ type: 'success', message: '凭据保存成功，已通过验证并开始拉取数据' });
      form.resetFields(['secret']);
    } catch (err) {
      setResult({
        type: 'error',
        message: err instanceof Error ? err.message : '保存失败，请检查 AppID 和 AppSecret 是否正确',
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div style={{ maxWidth: 560, display: 'flex', flexDirection: 'column', gap: 24 }}>
      {/* Status Card */}
      <div className="glass-card animate-in animate-in-1" style={{ padding: 28 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 20 }}>
          <div style={{
            width: 40,
            height: 40,
            borderRadius: 'var(--radius-md)',
            background: status?.configured ? 'var(--jade-glow)' : 'rgba(255,255,255,0.04)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }}>
            {status?.configured ? (
              <CheckCircleOutlined style={{ color: 'var(--jade-300)', fontSize: 18 }} />
            ) : (
              <CloseCircleOutlined style={{ color: 'var(--text-tertiary)', fontSize: 18 }} />
            )}
          </div>
          <div>
            <div style={{
              fontFamily: 'var(--font-display)',
              fontSize: 18,
              fontWeight: 600,
              color: 'var(--text-primary)',
            }}>
              连接状态
            </div>
            <div style={{ fontSize: 12, color: 'var(--text-tertiary)', marginTop: 2 }}>
              {status?.configured
                ? `已连接 · AppID: ${status.appid}`
                : '未配置 · 请输入微信公众号凭据'}
            </div>
          </div>
        </div>
      </div>

      {/* Credential Form */}
      <div className="glass-card animate-in animate-in-3" style={{ padding: 28 }}>
        <div className="chart-title" style={{ marginBottom: 24 }}>微信公众号凭据</div>

        {result && (
          <Alert
            message={result.type === 'success' ? '配置成功' : '配置失败'}
            description={result.message}
            type={result.type}
            showIcon
            closable
            onClose={() => setResult(null)}
            style={{ marginBottom: 20 }}
          />
        )}

        <Form
          form={form}
          layout="vertical"
          onFinish={handleSave}
          requiredMark={false}
        >
          <Form.Item
            name="appid"
            label={<span style={{ color: 'var(--text-secondary)', fontSize: 12, letterSpacing: '0.5px' }}>APPID</span>}
            rules={[{ required: true, message: '请输入 AppID' }]}
          >
            <Input
              prefix={<AppstoreOutlined style={{ color: 'var(--text-tertiary)' }} />}
              placeholder="wx1234567890abcdef"
              size="large"
              style={{
                background: 'var(--bg-primary)',
                borderColor: 'var(--border-card)',
                color: 'var(--text-primary)',
                fontFamily: 'var(--font-mono)',
              }}
            />
          </Form.Item>

          <Form.Item
            name="secret"
            label={<span style={{ color: 'var(--text-secondary)', fontSize: 12, letterSpacing: '0.5px' }}>APPSECRET</span>}
            rules={[{ required: true, message: '请输入 AppSecret' }]}
          >
            <Input.Password
              prefix={<KeyOutlined style={{ color: 'var(--text-tertiary)' }} />}
              placeholder="输入 AppSecret"
              size="large"
              style={{
                background: 'var(--bg-primary)',
                borderColor: 'var(--border-card)',
                color: 'var(--text-primary)',
                fontFamily: 'var(--font-mono)',
              }}
            />
          </Form.Item>

          <Space style={{ marginTop: 8 }}>
            <Button
              type="primary"
              htmlType="submit"
              loading={saving}
              size="large"
              style={{
                background: 'var(--jade-400)',
                borderColor: 'var(--jade-400)',
                fontWeight: 500,
              }}
            >
              {saving ? '验证中...' : '保存并验证'}
            </Button>
          </Space>
        </Form>

        <div style={{
          marginTop: 24,
          padding: 16,
          background: 'rgba(88, 166, 255, 0.06)',
          borderRadius: 'var(--radius-md)',
          border: '1px solid rgba(88, 166, 255, 0.1)',
        }}>
          <div style={{ fontSize: 12, color: 'var(--sky)', marginBottom: 8, fontWeight: 500 }}>
            如何获取凭据？
          </div>
          <ol style={{
            fontSize: 12,
            color: 'var(--text-tertiary)',
            paddingLeft: 16,
            margin: 0,
            lineHeight: 2,
          }}>
            <li>登录 <span style={{ color: 'var(--text-secondary)' }}>微信公众平台</span> (mp.weixin.qq.com)</li>
            <li>进入 <span style={{ color: 'var(--text-secondary)' }}>设置与开发 → 基本配置</span></li>
            <li>复制 <span style={{ color: 'var(--text-secondary)' }}>开发者ID(AppID)</span> 和 <span style={{ color: 'var(--text-secondary)' }}>开发者密码(AppSecret)</span></li>
          </ol>
        </div>
      </div>
    </div>
  );
}
