import React, { useMemo, useState } from 'react';
import { marked } from 'marked';
import './style.css';

const defaultSpec = {
  topic: '',
  outline: '',
  tone: '',
  audience: '',
  words: '',
  constraints: '',
};

function App() {
  const [spec, setSpec] = useState(defaultSpec);
  const [comment, setComment] = useState('');
  const [sessionId, setSessionId] = useState(null);
  const [draft, setDraft] = useState({ markdown: '' });
  const [history, setHistory] = useState([]);
  const [status, setStatus] = useState('等待生成...');
  const [loading, setLoading] = useState(false);

  const payload = useMemo(() => ({
    topic: spec.topic.trim(),
    outline: spec.outline.split('\n').filter(Boolean),
    tone: spec.tone.trim(),
    audience: spec.audience.trim(),
    words: parseInt(spec.words, 10) || 0,
    constraints: spec.constraints.split('\n').filter(Boolean),
  }), [spec]);

  const handleCreate = async () => {
    setLoading(true);
    setStatus('生成中...');
    const res = await fetch('/api/sessions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!res.ok) return handleError(res);
    const data = await res.json();
    applySession(data);
    setStatus('首稿生成完成');
    setLoading(false);
  };

  const handleRevise = async () => {
    if (!sessionId) return;
    setLoading(true);
    setStatus('修订中...');
    const res = await fetch(`/api/sessions/${sessionId}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ comment: comment.trim() }),
    });
    if (!res.ok) return handleError(res);
    const data = await res.json();
    applySession(data);
    setStatus('修订完成');
    setLoading(false);
  };

  const applySession = (data) => {
    setSessionId(data.session_id);
    setDraft(data.draft || {});
    setHistory(data.history || []);
  };

  const handleError = async (res) => {
    const text = await res.text();
    setStatus(`错误: ${res.status} ${text}`);
    setLoading(false);
  };

  const copyMd = async () => {
    await navigator.clipboard.writeText(draft.markdown || '');
    setStatus('已复制 Markdown');
  };

  const renderHistory = () => (history || []).map((t, idx) => {
    const ts = t.created_at ? new Date(t.created_at).toLocaleString() : `#${idx + 1}`;
    return (
      <div key={idx} className="history-item">
        <span className="badge">{ts}</span>{t.comment || t.summary || ''}
      </div>
    );
  });

  return (
    <main className="grid">
      <section className="card">
        <label>主题</label>
        <input value={spec.topic} onChange={e => setSpec({ ...spec, topic: e.target.value })} placeholder="例如：微信图文发布自动化实践" />
        <label>大纲（每行一条，选填）</label>
        <textarea value={spec.outline} onChange={e => setSpec({ ...spec, outline: e.target.value })} placeholder={'引言\n整体流程\n踩坑 & 经验'} />
        <label>语气 / 受众 / 字数</label>
        <div className="actions">
          <input style={{ flex: 1 }} value={spec.tone} onChange={e => setSpec({ ...spec, tone: e.target.value })} placeholder="专业、友好" />
          <input style={{ flex: 1 }} value={spec.audience} onChange={e => setSpec({ ...spec, audience: e.target.value })} placeholder="运营、开发" />
          <input style={{ width: 90 }} value={spec.words} onChange={e => setSpec({ ...spec, words: e.target.value })} placeholder="1200" />
        </div>
        <label>额外约束（每行一条）</label>
        <textarea value={spec.constraints} onChange={e => setSpec({ ...spec, constraints: e.target.value })} placeholder={'禁止使用第一人称\n每节加小结'} />
        <div className="actions">
          <button onClick={handleCreate} disabled={loading}>生成首稿</button>
          <button onClick={handleRevise} disabled={loading || !sessionId}>基于评论修订</button>
        </div>
        <label>评论 / 追加要求</label>
        <textarea value={comment} onChange={e => setComment(e.target.value)} placeholder="例：加强案例部分，补充图片占位说明" />
        <div className="meta">{sessionId ? `Session: ${sessionId}` : '尚未创建 Session'}</div>
      </section>

      <section className="card">
        <div className="actions" style={{ justifyContent: 'space-between', alignItems: 'center' }}>
          <div className="meta">{status}</div>
          <button onClick={copyMd} disabled={!draft.markdown}>复制 Markdown</button>
        </div>
        <div className="preview markdown-body" dangerouslySetInnerHTML={{ __html: marked.parse(draft.markdown || '') }} />
        <div className="history">{renderHistory()}</div>
      </section>
    </main>
  );
}

export default App;
