import React, { useEffect, useMemo, useRef, useState } from 'react';
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
  const [cover, setCover] = useState({ path: '', url: '', filename: '' });
  const [bodyImages, setBodyImages] = useState([]);
  const [uploading, setUploading] = useState(false);
  const [showConstraints, setShowConstraints] = useState(false);

  const coverInputRef = useRef(null);
  const bodyInputRef = useRef(null);
  const heartbeatRef = useRef(null);

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
      body: JSON.stringify({
        session_id: sessionId,
        cover_path: cover.path || undefined,
        title: draft.title,
        digest: draft.digest,
      }),
    });
    if (!res.ok) return handleError(res, true);
    const data = await res.json();
    // 不在前端显示 media_id，防止误泄露；仅提示成功。
    setStatus('发布成功');
    setPublishing(false);
  };

  const deleteSession = async () => {
    if (!sessionId) return;
    try {
      await fetch(`/api/sessions/${sessionId}`, { method: 'DELETE' });
    } catch (err) {
      console.error('delete session failed', err);
    }
  };

  // Heartbeat: keep session alive while page is open
  useEffect(() => {
    if (!sessionId) {
      if (heartbeatRef.current) {
        clearInterval(heartbeatRef.current);
        heartbeatRef.current = null;
      }
      return;
    }
    const sendBeat = async () => {
      try {
        await fetch(`/api/heartbeat/${sessionId}`, { method: 'POST' });
      } catch (err) {
        console.debug('heartbeat failed', err);
      }
    };
    sendBeat();
    heartbeatRef.current = setInterval(sendBeat, 60_000); // 60s
    return () => {
      if (heartbeatRef.current) {
        clearInterval(heartbeatRef.current);
        heartbeatRef.current = null;
      }
    };
  }, [sessionId]);

  const uploadFile = async (file, usage = 'content') => {
    const formData = new FormData();
    formData.append('file', file);
    formData.append('usage', usage);
    if (sessionId) formData.append('session_id', sessionId);
    const res = await fetch('/api/uploads', { method: 'POST', body: formData });
    if (!res.ok) {
      const msg = await res.text();
      throw new Error(msg || '上传失败');
    }
    return res.json();
  };

  const handleCoverSelect = async (e) => {
    if (!sessionId) {
      setStatus('请先生成草稿再上传封面');
      e.target.value = '';
      return;
    }
    const file = e.target.files?.[0];
    if (!file) return;
    setUploading(true);
    setStatus('封面上传中...');
    try {
      const data = await uploadFile(file, 'cover');
      setCover({ path: data.path, url: data.url, filename: data.filename });
      setStatus('封面已上传');
    } catch (err) {
      setStatus(`错误: ${err.message}`);
    } finally {
      setUploading(false);
      e.target.value = '';
    }
  };

  const handleBodySelect = async (e) => {
    if (!sessionId) {
      setStatus('请先生成草稿再上传图片');
      e.target.value = '';
      return;
    }
    const files = Array.from(e.target.files || []);
    if (!files.length) return;
    setUploading(true);
    setStatus('正文图片上传中...');
    try {
      const results = [];
      for (const f of files) {
        // 逐个上传，避免过多并发
        const data = await uploadFile(f, 'content');
        results.push({ path: data.path, url: data.url, filename: data.filename });
      }
      setBodyImages((prev) => [...prev, ...results]);
      setStatus(`已上传 ${files.length} 张正文图片`);
    } catch (err) {
      setStatus(`错误: ${err.message}`);
    } finally {
      setUploading(false);
      e.target.value = '';
    }
  };

  const editorRef = useRef(null);

  const insertImageIntoMarkdown = (img) => {
    if (!img?.path) return;
    insertSnippet(`![正文图片](${img.path})`);
  };

  const insertSnippet = (snippet) => {
    setDraft((prev) => {
      const md = prev.markdown || '';
      const editor = editorRef.current;
      if (editor) {
        const start = editor.selectionStart ?? md.length;
        const end = editor.selectionEnd ?? md.length;
        const next =
          md.slice(0, start) + snippet + (md[start - 1] === '\n' ? '' : '\n') + md.slice(end);
        // 让光标落在插入内容之后
        requestAnimationFrame(() => {
          const pos = start + snippet.length + 1;
          editor.selectionStart = editor.selectionEnd = pos;
        });
        return { ...prev, markdown: next };
      }
      const next = md ? `${md}\n\n${snippet}\n` : `${snippet}\n`;
      return { ...prev, markdown: next };
    });
    setStatus('已插入图片');
  };

  const applySession = (data) => {
    const rawDraft = data.draft || {};
    const normalizedDraft = {
      markdown: rawDraft.markdown || rawDraft.Markdown || '',
      title: rawDraft.title || rawDraft.Title || '',
      digest: rawDraft.digest || rawDraft.Digest || '',
    };
    const normalizedHistory = (data.history || []).map((h) => {
      const baseSummary = h.summary || h.Summary || '';
      const friendly = baseSummary.toLowerCase().includes('initial') ? '首次生成' : baseSummary;
      const baseComment = h.comment || h.Comment || '';
      return {
        comment: baseComment || friendly,
        summary: friendly || baseSummary,
        created_at: h.created_at || h.CreatedAt || '',
      };
    });
    setSessionId(data.session_id);
    setDraft(normalizedDraft);
    setHistory(normalizedHistory);
    if (normalizedDraft.markdown) {
      setStatus((prev) => prev.includes('错误') ? prev : '草稿已就绪，可编辑插图');
    }
  };

  const handleError = async (res, isPublish = false) => {
    const text = await res.text();
    setStatus(`错误: ${res.status} ${text}`);
    if (isPublish) setPublishing(false);
    else setLoading(false);
  };

  const statusTone = useMemo(() => {
    if (loading) return 'info';
    if (status.includes('错误')) return 'danger';
    if (status.includes('完成') || status.includes('已复制')) return 'success';
    return 'neutral';
  }, [status, loading]);

  const clippedStatus = useMemo(() => {
    const limit = 120;
    if ((status || '').length <= limit) return status;
    return `${status.slice(0, limit)}...`;
  }, [status]);

  const previewHTML = useMemo(() => {
    const uploadMap = [...bodyImages, cover].filter(Boolean);
    let md = draft.markdown || '';
    uploadMap.forEach((u) => {
      if (u.path && u.url) {
        md = md.split(u.path).join(u.url);
      }
    });
    return marked.parse(md || '');
  }, [draft.markdown, bodyImages, cover]);

  const renderHistory = () => (history || []).map((t, idx) => {
    const ts = t.created_at ? new Date(t.created_at).toLocaleString() : `#${idx + 1}`;
    return (
      <div key={idx} className="history-item">
        <span className="badge">{ts}</span>
        <span className="history-text">{t.comment || t.summary || ''}</span>
      </div>
    );
  });

  const canUploadCover = !!sessionId && !uploading;
  const coverHint = sessionId ? '支持 JPG / PNG，点击选择' : '需先生成草稿再上传';

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
            <span>一键发布</span>
          </div>
        </header>

        <main className="grid">
          {/* 左列：状态/发布 + 灵感设定 */}
          <div className="col-4 stacked-col">
            <section className="card card-ghost status-card">
              <div className="section-title status-row">
                <div className={`status-pill status-${statusTone}`} title={status}>{clippedStatus}</div>
                <div className="actions">
                  <button className="btn btn-secondary" onClick={handlePublish} disabled={!draft.markdown || publishing || uploading}>发布到草稿箱</button>
                </div>
              </div>
            </section>

            <section className="card card-solid">
              <div className="section-title">
                <span className="dot" />
                灵感设定
              </div>
              <label>主题</label>
              <input value={spec.topic} onChange={e => setSpec({ ...spec, topic: e.target.value })} placeholder="例如：微信图文发布自动化实践" />
              <label>背景信息</label>
              <textarea
                value={spec.outline}
                onChange={e => setSpec({ ...spec, outline: e.target.value })}
                placeholder={`资料要点/链接/案例\n可多行，每行一条`}
              />
              <div className="inline-field">
                <label>目标字数</label>
                <input
                  className="compact"
                  value={spec.words}
                  onChange={e => setSpec({ ...spec, words: e.target.value })}
                  placeholder="如 1200"
                />
              </div>
              <div className="constraints-toggle">
                <label>写作指南</label>
                <button
                  type="button"
                  className="btn btn-ghost compact-btn"
                  onClick={() => setShowConstraints((v) => !v)}
                >
                  {showConstraints ? '收起' : '添加'}
                </button>
              </div>
              {showConstraints && (
                <textarea
                  value={spec.constraints}
                  onChange={e => setSpec({ ...spec, constraints: e.target.value })}
                  placeholder={`禁止使用第一人称\n每节加小结`}
                />
              )}
              <div className="actions spaced">
                <button className="btn btn-primary" onClick={handleSubmit} disabled={loading}>
                  {sessionId ? '基于评论更新' : '生成首稿'}
                </button>
                <button
                  className="btn btn-ghost"
                  onClick={() => {
                    deleteSession();
                    setSpec(defaultSpec);
                    setComment('');
                    setSessionId(null);
                    setDraft({ markdown: '' });
                    setHistory([]);
                    setCover({ path: '', url: '', filename: '' });
                    setBodyImages([]);
                    setStatus('等待生成...');
                  }}
                  disabled={loading}
                >
                  重置
                </button>
              </div>
              <label>评论 / 追加要求</label>
              <textarea value={comment} onChange={e => setComment(e.target.value)} placeholder="例：加强案例部分，补充图片占位说明" />
            </section>
          </div>

          {/* 右列：上半编辑预览，下半素材管理 */}
          <div className="col-8 stacked-col">
            <section className="card card-ghost preview-card">
              <div className="editor-card">
                <div className="section-title">
                  <span className="dot" />
                  正文编辑（可定位插图）
                </div>
                <textarea
                  ref={editorRef}
                  className="md-editor"
                  value={draft.markdown || ''}
                  onChange={e => setDraft({ ...draft, markdown: e.target.value })}
                  placeholder="点击定位光标，可粘贴或插入图片 Markdown。"
                  onDrop={(e) => {
                    e.preventDefault();
                    const snippet = e.dataTransfer.getData('text/plain');
                    if (snippet) insertSnippet(snippet);
                  }}
                />
              </div>

              {draft.markdown ? (
                <div
                  className="preview markdown-body"
                  onDragOver={(e) => e.preventDefault()}
                  onDrop={(e) => {
                    e.preventDefault();
                    const snippet = e.dataTransfer.getData('text/plain');
                    if (snippet) {
                      // 插入前先聚焦编辑器，确保落点以当前光标为准
                      editorRef.current?.focus();
                      insertSnippet(snippet);
                    }
                  }}
                  title="可拖拽图片插入；精确位置请在上方文本框定位后插入"
                  dangerouslySetInnerHTML={{ __html: previewHTML }}
                />
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

            <section className="card card-ghost media-stack">
              <div className="section-title">
                <span className="dot" />
                素材管理
              </div>
              <div className="media-panel media-panel-grid">
                <div className="media-card">
                  <div className="section-title">
                    <span className="dot" />
                    封面图
                  </div>
                  <div
                    className={`upload-tile card-click ${canUploadCover ? '' : 'disabled'}`}
                    onClick={() => {
                      if (canUploadCover) {
                        coverInputRef.current?.click();
                      } else {
                        setStatus('请先生成草稿再上传封面');
                      }
                    }}
                  >
                    {cover.url ? (
                      <>
                        <img src={cover.url} alt="cover" className="cover-preview" />
                        <div className="upload-meta">
                          <div className="upload-name">封面已上传</div>
                          <div className="upload-hint">点击可重新选择</div>
                        </div>
                      </>
                    ) : (
                      <div className="upload-empty">
                        <div className="empty-icon">🖼️</div>
                        <div className="empty-title">上传封面</div>
                        <div className="upload-hint">{coverHint}</div>
                      </div>
                    )}
                  </div>
                  <input ref={coverInputRef} type="file" accept="image/*" onChange={handleCoverSelect} hidden disabled={!canUploadCover} />
                </div>

                <div className="media-card">
                  <div className="section-title">
                    <span className="dot" />
                    正文图片
                  </div>
                  <div className="actions spaced">
                    <button className="btn btn-primary" onClick={() => bodyInputRef.current?.click()} disabled={!sessionId || uploading}>上传正文图片</button>
                  </div>
                  <input ref={bodyInputRef} type="file" accept="image/*" multiple hidden onChange={handleBodySelect} />
                  {bodyImages.length ? (
                    <div className="media-list horizontal-scroll">
                      {bodyImages.map((img, idx) => (
                        <div className="media-item" key={`${img.path}-${idx}`}>
                          <img
                            src={img.url}
                            alt="body-img"
                            draggable
                            onDragStart={(e) => {
                              e.dataTransfer.setData('text/plain', `![正文图片](${img.path})`);
                              e.dataTransfer.effectAllowed = 'copy';
                            }}
                            onClick={() => window.open(img.url, '_blank', 'noopener')}
                          />
                          <div className="media-meta">
                            <div className="upload-name">正文图片</div>
                            <div className="actions">
                              <button className="btn btn-ghost compact-btn" onClick={() => insertImageIntoMarkdown(img)}>插入正文</button>
                            </div>
                          </div>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <div className="empty-inline">还没有正文图片</div>
                  )}
                </div>
              </div>
            </section>
          </div>
        </main>
      </div>
    </div>
  );
}

export default App;
