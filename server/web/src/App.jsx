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
  const [publishing, setPublishing] = useState(false);

  const payload = useMemo(() => ({
    topic: spec.topic.trim(),
    outline: spec.outline.split('\n').filter(Boolean),
    tone: spec.tone.trim(),
    audience: spec.audience.trim(),
    words: parseInt(spec.words, 10) || 0,
    constraints: spec.constraints.split('\n').filter(Boolean),
  }), [spec]);

  const handleSubmit = async () => {
    setLoading(true);
    const isNew = !sessionId;
    setStatus(isNew ? '生成中...' : '修订中...');
    const res = await fetch(isNew ? '/api/sessions' : `/api/sessions/${sessionId}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(isNew ? payload : { comment: comment.trim() }),
    });
    if (!res.ok) return handleError(res);
    const data = await res.json();
    applySession(data);
    setStatus(isNew ? '首稿生成完成' : '修订完成');
    setLoading(false);
  };

  const handlePublish = async () => {
    if (!sessionId || !draft.markdown) {
      setStatus('请先生成稿件');
      return;
    }
    setPublishing(true);
    setStatus('发布中...');
    const res = await fetch('/api/publish', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ session_id: sessionId }),
    });
    if (!res.ok) return handleError(res, true);
    const data = await res.json();
    setStatus(`发布成功：${data.media_id || '草稿已创建'}`);
    setPublishing(false);
  };

  const applySession = (data) => {
    const rawDraft = data.draft || {};
    const normalizedDraft = {
      markdown: rawDraft.markdown || rawDraft.Markdown || '',
      title: rawDraft.title || rawDraft.Title || '',
      digest: rawDraft.digest || rawDraft.Digest || '',
    };
    const normalizedHistory = (data.history || []).map((h) => ({
      comment: h.comment || h.Comment || h.summary || h.Summary || '',
      summary: h.summary || h.Summary || '',
      created_at: h.created_at || h.CreatedAt || '',
    }));
    setSessionId(data.session_id);
    setDraft(normalizedDraft);
    setHistory(normalizedHistory);
  };

  const handleError = async (res, isPublish = false) => {
    const text = await res.text();
    setStatus(`错误: ${res.status} ${text}`);
    if (isPublish) setPublishing(false);
    else setLoading(false);
  };

  const copyMd = async () => {
    if (!draft.markdown) return;
    await navigator.clipboard.writeText(draft.markdown || '');
    setStatus('已复制 Markdown');
  };

  const statusTone = useMemo(() => {
    if (loading) return 'info';
    if (status.includes('错误')) return 'danger';
    if (status.includes('完成') || status.includes('已复制')) return 'success';
    return 'neutral';
  }, [status, loading]);

  const renderHistory = () => (history || []).map((t, idx) => {
    const ts = t.created_at ? new Date(t.created_at).toLocaleString() : `#${idx + 1}`;
    return (
      <div key={idx} className="history-item">
        <span className="badge">{ts}</span>
        <span className="history-text">{t.comment || t.summary || ''}</span>
      </div>
    );
  });

  return (
    <div className="page">
      <div className="aurora aurora-1" />
      <div className="aurora aurora-2" />
      <div className="container">
        <header className="hero card card-accent">
          <div className="logo">写作工坊·光谱</div>
          <p className="subtitle">把灵感交给次元助手，自动生成可发布的微信图文。</p>
          <div className="chips">
            <span>主题塑形</span>
            <span>自动修订</span>
            <span>Markdown 一键复制</span>
          </div>
        </header>

        <main className="grid">
          <section className="card card-solid col-4">
            <div className="section-title">
              <span className="dot" />
              灵感设定
            </div>
            <label>主题</label>
            <input value={spec.topic} onChange={e => setSpec({ ...spec, topic: e.target.value })} placeholder="例如：微信图文发布自动化实践" />
            <label>大纲（每行一条，选填）</label>
            <textarea value={spec.outline} onChange={e => setSpec({ ...spec, outline: e.target.value })} placeholder={'引言\n整体流程\n踩坑 & 经验'} />
            <label>目标字数</label>
            <div className="actions stacked">
              <input className="compact" value={spec.words} onChange={e => setSpec({ ...spec, words: e.target.value })} placeholder="如 1200" />
            </div>
          <label>额外约束（每行一条）</label>
          <textarea value={spec.constraints} onChange={e => setSpec({ ...spec, constraints: e.target.value })} placeholder={'禁止使用第一人称\n每节加小结'} />
          <div className="actions spaced">
            <button className="btn btn-primary" onClick={handleSubmit} disabled={loading}>
              {sessionId ? '基于评论更新' : '生成首稿'}
            </button>
            <button className="btn btn-ghost" onClick={() => { setSpec(defaultSpec); setComment(''); setSessionId(null); setDraft({ markdown: '' }); setHistory([]); setStatus('等待生成...'); }} disabled={loading}>
              重置
            </button>
          </div>
            <label>评论 / 追加要求</label>
            <textarea value={comment} onChange={e => setComment(e.target.value)} placeholder="例：加强案例部分，补充图片占位说明" />
          </section>

          <section className="card card-ghost col-8">
            <div className="section-title status-row">
              <div className={`status-pill status-${statusTone}`}>{status}</div>
              <div className="actions">
                <button className="btn btn-secondary" onClick={handlePublish} disabled={!draft.markdown || publishing}>发布到草稿箱</button>
                <button className="btn btn-ghost" onClick={copyMd} disabled={!draft.markdown}>复制 Markdown</button>
              </div>
            </div>
            {draft.markdown ? (
              <div className="preview markdown-body" dangerouslySetInnerHTML={{ __html: marked.parse(draft.markdown || '') }} />
            ) : (
              <div className="empty-state">
                <div className="empty-icon">✦</div>
                <div className="empty-title">暂无内容</div>
                <div className="empty-desc">设置好主题后点击“生成首稿”即可预览 Markdown。</div>
              </div>
            )}
            <div className="history">
              <div className="section-title">
                <span className="dot" />
                修订轨迹
              </div>
              {history.length ? renderHistory() : (
                <div className="empty-inline">还没有修订记录，添加评论后尝试“基于评论修订”。</div>
              )}
            </div>
          </section>
        </main>
      </div>
    </div>
  );
}

export default App;
