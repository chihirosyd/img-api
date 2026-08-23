package handler

// 本文件集中提示页的 HTML 模板常量，避免挤占 api.go 的处理逻辑。
//
//   - homePage             — 根路径 / 的教程首页（{{HOST}} 由 Home 动态替换）
//   - setupGuidePage       — 图源未配置时的"开始使用"引导页
//   - categoryNotFoundPage — 分类不存在时的提示页（{{CATEGORY}} / {{AVAILABLE}} 由 renderCategoryNotFound 动态替换）

// homePage 是访问根路径 / 时的项目首页（教程页）。
// {{HOST}} 由 Home handler 替换为当前请求的 Host，用于展示完整示例 URL。
// {{STATUS}} 由 Home handler 注入运行状态区块（见 homeStatusHTML）。
const homePage = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>img-api · 随机图片 API</title>
<style>
  body { font-family: -apple-system, "Segoe UI", "Microsoft YaHei", sans-serif;
         background: linear-gradient(160deg, #eaf2ff 0%, #f6f7f9 45%, #f3f1ff 100%);
         min-height: 100vh; margin: 0; padding: 40px 16px; color: #333; }
  .card { max-width: 760px; margin: 0 auto; background: #fff;
          border: 1px solid #e4e7eb; border-radius: 16px; padding: 36px 40px;
          box-shadow: 0 8px 30px rgba(31,45,90,.08); position: relative; overflow: hidden; }
  .card::before { content: ""; display: block; height: 4px; margin: -36px -40px 24px;
                  background: linear-gradient(90deg, #2f81f7, #7c3aed); }
  h1 { font-size: 25px; margin: 0 0 6px; letter-spacing: .5px; }
  h2 { font-size: 16px; margin: 28px 0 10px; border-left: 4px solid #2f81f7;
       padding-left: 10px; color: #1f2937; }
  p { line-height: 1.8; font-size: 14px; margin: 8px 0; }
  .sub { color: #666; font-size: 13px; margin: 0 0 6px; }
  code { background: #f1f3f5; border-radius: 5px; padding: 2px 7px;
         font-family: Consolas, Monaco, monospace; font-size: 13px; color: #c7254e; }
  pre { background: #282c34; color: #abb2bf; border-radius: 10px;
        padding: 14px 18px; overflow-x: auto; font-size: 13px; line-height: 1.7;
        box-shadow: inset 0 1px 3px rgba(0,0,0,.25); }
  pre code { background: none; color: inherit; padding: 0; }
  .tip { background: #eef6ff; border-left: 4px solid #2f81f7; border-radius: 6px;
         padding: 10px 14px; font-size: 13px; margin-top: 24px; }

  /* 运行状态 */
  .status-line { font-size: 15px; font-weight: 600; color: #1f2937; margin: 2px 0 4px; }
  .status-line .dot { display: inline-block; width: 10px; height: 10px; border-radius: 50%;
                      background: #22c55e; margin-right: 9px;
                      animation: pulse 2s ease-out infinite; }
  .status-line .ver { font-size: 12px; font-weight: 500; color: #2f81f7;
                      background: #eef6ff; border-radius: 999px;
                      padding: 2px 10px; margin-left: 8px; }
  @keyframes pulse { 0%,100% { box-shadow: 0 0 0 0 rgba(34,197,94,.35); }
                     50% { box-shadow: 0 0 0 6px rgba(34,197,94,0); } }
  .status-table { width: 100%; border-collapse: collapse; font-size: 13px; margin: 10px 0 4px; }
  .status-table td { padding: 7px 8px; border-bottom: 1px solid #f0f2f6; }
  .status-table td:first-child { width: 90px; color: #8a94a6; }
  .badge { display: inline-block; padding: 2px 10px; border-radius: 999px; font-size: 12px; }
  .badge.ok   { background: #e6f7ec; color: #16803c; }
  .badge.warn { background: #fef3e2; color: #b45309; }
  .badge.gray { background: #f1f3f5; color: #666; }
  .stat-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 10px; margin: 10px 0 2px; }
  .stat-card { background: #f8fafc; border: 1px solid #edf0f5; border-radius: 10px;
               padding: 10px 6px; text-align: center; }
  .stat-card .num { font-size: 20px; font-weight: 700; color: #1f2937; line-height: 1.2; }
  .stat-card .label { font-size: 12px; color: #8a94a6; margin-top: 3px; }

  /* 深色模式 */
  @media (prefers-color-scheme: dark) {
    body { background: #0f1115; }
    .card { background: #171a21; border-color: #262b36; }
    h1, h2, .status-line, .stat-card .num { color: #e6e8ec; }
    p, .sub, td { color: #a7adb8; }
    code { background: #262b36; color: #7db3ff; }
    .status-table td { border-color: #262b36; }
    .stat-card { background: #1d222c; border-color: #262b36; }
    .stat-card .label, .status-table td:first-child { color: #6d7686; }
    .tip { background: #131c2e; }
    .badge.gray { background: #262b36; color: #a7adb8; }
  }

  /* 移动端 */
  @media (max-width: 560px) {
    .card { padding: 28px 22px; }
    .card::before { margin: -28px -22px 20px; }
    .stat-grid { grid-template-columns: repeat(2, 1fr); }
  }
</style>
</head>
<body>
<div class="card">
  <h1>🎲 img-api 随机图片 API</h1>
  <p class="sub">服务运行中。把图片地址嵌入博客，每次访问随机返回一张图片。</p>

  <h2>运行状态</h2>
  {{STATUS}}

  <h2>三步上手</h2>
  <p>1️⃣ 打开 <code>http://{{HOST}}/random</code>，浏览器会直接跳转到一张随机图片</p>
  <p>2️⃣ 博客/网页嵌入：</p>
  <pre><code>&lt;img src="http://{{HOST}}/random" alt="随机图片"&gt;</code></pre>
  <p>3️⃣ 指定分类（分类名 = 图库 txt 文件名）：</p>
  <pre><code>http://{{HOST}}/random?category=风景</code></pre>

  <h2>常用参数</h2>
  <pre><code>type    auto / pc / pe            设备类型（手机竖屏用 pe）
source  txt / local / external    图片来源
mode    redirect / json / image   返回模式
category 分类名                   逗号多选，如 anime,scenery</code></pre>

  <h2>完整文档</h2>
  <p>详见仓库 <code>docs/API.md</code>（参数详解、响应格式、状态码速查）。</p>

  <div class="tip">💡 图源还没有图片时，访问 /random 会显示"开始使用"引导页，
       按提示添加图片即可。</div>
</div>
</body>
</html>`

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
  <p>新增分类（新目录）即时生效；已有分类中增删图片需重建索引并重启加载。</p>

  <h2>方式三：外部 API 池</h2>
  <p>编辑 <code>config/image.yaml</code>，在 <code>external_apis</code> 下添加 API 端点：</p>
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
  <div class="tip">💡 API 名称对应 <code>config/image.yaml</code> 中 <code>external_apis</code> 各项的
      <code>name</code> 字段，例如 <code>?source=external&amp;api=flickr</code>。</div>
</div>
</body>
</html>`
