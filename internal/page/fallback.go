package page

// FallbackHTML is served when netmanager has never answered for an address, or
// has been unreachable for longer than the cache may reuse an old page.
var FallbackHTML = []byte(`<!doctype html>
<html lang="id">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Layanan diisolir</title>
</head>
<body>
<p>Layanan internet Anda sedang diisolir. Silakan hubungi penyedia layanan Anda.</p>
</body>
</html>
`)
