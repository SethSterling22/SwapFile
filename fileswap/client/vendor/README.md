# Vendored libraries

Served by the FileSwap server itself, so the page loads no third-party scripts.

| File | Library | Version | Source | License |
|---|---|---|---|---|
| `qrcode.min.js` | [qrcodejs](https://github.com/davidshimjs/qrcodejs) — draws the QR codes | 1.0.0 | `https://cdnjs.cloudflare.com/ajax/libs/qrcodejs/1.0.0/qrcode.min.js` | MIT, © 2012 davidshimjs |

`index.html` loads it with Subresource Integrity (`sha512-CNgIRecGo7nphbeZ04Sc13ka07paqdeTu0WR1IM4kNcpmBAUSHSQX0FslNhTDadL4O5SAGapGt4FodqL8My0mA==`, the hash cdnjs publishes for that file). If you update it, update the hash too.
