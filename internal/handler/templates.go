package handler

// 本文件集中提示页的 HTML 模板常量，避免挤占 api.go 的处理逻辑。
//
//   - setupGuidePage       — 图源未配置时的"开始使用"引导页
//   - categoryNotFoundPage — 分类不存在时的提示页（{{CATEGORY}} / {{AVAILABLE}} 由 renderCategoryNotFound 动态替换）

// setupGuidePage 是图源未配置时展示的"开始使用"引导页（纯文字）。
const setupGuidePage = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>欢迎使用 img-api</title>
<style>
  body { font-family: -apple-system, "Segoe UI", "Microsoft YaHei", sans-serif;
         background: #f6f7f9; margin: 0; padding: 40px 16px; color: #333; }
  .card { max-width: 760px; margin: 0 auto; background: #fff;
          border: 1px solid #e4e7eb; border-radius: 12px; padding: 32px 40px;
          box-shadow: 0 2px 8px rgba(0,0,0,.05); }
  h1 { font-size: 24px; margin: 0 0 8px; }
  h2 { font-size: 17px; margin: 26px 0 8px; border-left: 4px solid #2f81f7;
       padding-left: 10px; }
  p { line-height: 1.8; font-size: 14px; }
  .sub { color: #666; font-size: 13px; margin: 0 0 20px; }
  code { background: #f1f3f5; border-radius: 4px; padding: 2px 6px;
         font-family: Consolas, Monaco, monospace; font-size: 13px; color: #c7254e; }
  pre { background: #282c34; color: #abb2bf; border-radius: 8px;
        padding: 14px 18px; overflow-x: auto; font-size: 13px; line-height: 1.7; }
  pre code { background: none; color: inherit; padding: 0; }
  .tip { background: #eef6ff; border-left: 4px solid #2f81f7; border-radius: 4px;
         padding: 10px 14px; font-size: 13px; margin-top: 20px; }
</style>
</head>
<body>
<div class="card">
  <h1>🎲 img-api 已就绪</h1>
  <p class="sub">服务正在运行，但你访问的图源还没有图片。按下面任意一种方式添加即可开始使用。</p>

  <h2>方式一：TXT 图库（最简单）</h2>
  <p>在 <code>resources/txt/pc/</code> 或 <code>resources/txt/pe/</code> 下新建
     <code>default.txt</code>，每行一个图片 URL：</p>
  <pre><code># 示例：resources/txt/pc/default.txt
https://example.com/photo1.jpg
https://example.com/photo2.jpg</code></pre>
  <p>新增文件即时生效，无需重启；每次访问都会随机返回一张图片。</p>

  <h2>方式二：本地图片</h2>
  <p>将图片放入 <code>resources/local/pc/default/</code> 或
     <code>resources/local/pe/default/</code>，目录结构如下：</p>
  <pre><code>resources/local/pc/default/
  ├── photo1.jpg
  └── photo2.png</code></pre>
  <p>新增分类（新目录）即时生效；已有分类中增删图片需重启服务或重建索引。</p>

  <h2>方式三：外部 API 池</h2>
  <p>编辑 <code>configs/image.yaml</code>，在 <code>external_apis</code> 下添加 API 端点：</p>
  <pre><code>external_apis:
  - name: picsum
    url: https://picsum.photos/{width}/{height}
    response_type: redirect</code></pre>
  <p>保存后重启服务生效。</p>

  <div class="tip">📚 完整配置说明见 <code>docs/CONFIG.md</code>，部署指南见 <code>docs/DEPLOY.md</code>。</div>
</div>
</body>
</html>`

// categoryNotFoundPage 是分类不存在时展示的提示页模板。
// {{CATEGORY}} 和 {{AVAILABLE}} 由 renderCategoryNotFound 动态替换。
const categoryNotFoundPage = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>分类不存在 · img-api</title>
<style>
  body { font-family: -apple-system, "Segoe UI", "Microsoft YaHei", sans-serif;
         background: #f6f7f9; margin: 0; padding: 40px 16px; color: #333; }
  .card { max-width: 680px; margin: 0 auto; background: #fff;
          border: 1px solid #e4e7eb; border-radius: 12px; padding: 32px 36px;
          box-shadow: 0 2px 8px rgba(0,0,0,.05); }
  h1 { font-size: 22px; margin: 0 0 12px; }
  .badge { display: inline-block; background: #fdeaea; color: #b91c1c;
           border: 1px solid #f5c6c6; border-radius: 6px; padding: 2px 10px;
           font-size: 13px; margin-left: 8px; vertical-align: middle; }
  p { line-height: 1.8; font-size: 14px; }
  code { background: #f1f3f5; border-radius: 4px; padding: 2px 6px;
         font-family: Consolas, Monaco, monospace; font-size: 13px; color: #c7254e; }
  .tip { background: #eef6ff; border-left: 4px solid #2f81f7; border-radius: 4px;
         padding: 10px 14px; font-size: 13px; }
</style>
</head>
<body>
<div class="card">
  <h1>🔍 分类不存在<span class="badge">404</span></h1>
  <p>你请求的分类 <code>{{CATEGORY}}</code> 在该图源中不存在。</p>
  <p>当前可用的分类：{{AVAILABLE}}</p>
  <div class="tip">💡 分类名对应 TXT 文件名（如 <code>anime</code> → <code>anime.txt</code>）
      或本地图片目录名、外部 API 配置的分类。可用逗号多选：<code>?category=分类1,分类2</code>。</div>
</div>
</body>
</html>`

// apiNotFoundPage 是指定的外部 API 名称不存在时的提示页模板。
// {{API}} 和 {{AVAILABLE}} 由 renderAPINotFound 动态替换。
const apiNotFoundPage = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>API 不存在 · img-api</title>
<style>
  body { font-family: -apple-system, "Segoe UI", "Microsoft YaHei", sans-serif;
         background: #f6f7f9; margin: 0; padding: 40px 16px; color: #333; }
  .card { max-width: 680px; margin: 0 auto; background: #fff;
          border: 1px solid #e4e7eb; border-radius: 12px; padding: 32px 36px;
          box-shadow: 0 2px 8px rgba(0,0,0,.05); }
  h1 { font-size: 22px; margin: 0 0 12px; }
  .badge { display: inline-block; background: #fdeaea; color: #b91c1c;
           border: 1px solid #f5c6c6; border-radius: 6px; padding: 2px 10px;
           font-size: 13px; margin-left: 8px; vertical-align: middle; }
  p { line-height: 1.8; font-size: 14px; }
  code { background: #f1f3f5; border-radius: 4px; padding: 2px 6px;
         font-family: Consolas, Monaco, monospace; font-size: 13px; color: #c7254e; }
  .tip { background: #eef6ff; border-left: 4px solid #2f81f7; border-radius: 4px;
         padding: 10px 14px; font-size: 13px; }
</style>
</head>
<body>
<div class="card">
  <h1>🔍 API 不存在<span class="badge">404</span></h1>
  <p>你指定的 API <code>{{API}}</code> 在外部 API 池中不存在。</p>
  <p>当前可用的 API：{{AVAILABLE}}</p>
  <div class="tip">💡 API 名称对应 <code>configs/image.yaml</code> 中 <code>external_apis</code> 各项的
      <code>name</code> 字段，例如 <code>?source=external&amp;api=flickr</code>。</div>
</div>
</body>
</html>`
