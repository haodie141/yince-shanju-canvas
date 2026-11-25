# Fada 图片生成

支持 Fada 的文生图和多参考图生图，使用独立协议插件。

接口：`POST https://apinocf.fdai.xyz/v1/images/generations`。请求体为 JSON，包含 `model`、`prompt`、`size` 和可选参考图 URL 数组 `image`；以 Bearer API Key 鉴权。

上游可能在 `data[].b64_json` 中返回图片 URL，也可能返回 Base64；插件分别识别并交由宿主持久化。
