# netmanager-isolir

Layanan kecil yang menampilkan halaman isolir kepada pelanggan yang layanannya
dihentikan. Router VPN mengarahkan lalu lintas HTTP/HTTPS pelanggan terisolir
ke layanan ini; layanan mengambil halamannya dari netmanager
(`GET /api/internal/isolir/page?address=<ipv4>`), menyimpannya di cache (60
detik, dipakai sampai 24 jam bila netmanager gagal), lalu memakai halaman
cadangan bila belum pernah berhasil. Tidak ada database, dan tidak ada koneksi
keluar selain ke netmanager.

## Port

| Port | Fungsi | NodePort |
|------|--------|----------|
| 8080 | HTTP   | 30880    |
| 8443 | HTTPS  | 30443    |
| 8081 | `/healthz` (hanya untuk probe) | - |

## Konfigurasi (env)

| Variabel | Isi |
|----------|-----|
| `NETMANAGER_URL` | Alamat netmanager, mis. `http://netmanager-app.netmanager-production.svc.cluster.local` |
| `NETMANAGER_TOKEN` | Token bearer, minimal 32 karakter; sama dengan `ISOLIR_SERVICE_TOKEN` di netmanager |
| `TLS_CERT_FILE` | Path sertifikat (dipasang dari Secret sebagai file) |
| `TLS_KEY_FILE` | Path kunci privat |

Semuanya wajib; layanan berhenti saat start bila ada yang kosong.

## Sertifikat dan Secret

Satu sertifikat self-signed melayani semua nama; SNI tidak diperiksa.

```sh
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 3650 \
  -subj "/CN=isolir" -keyout tls.key -out tls.crt

kubectl create namespace netmanager-isolir
kubectl -n netmanager-isolir create secret tls netmanager-isolir-tls \
  --cert=tls.crt --key=tls.key
kubectl -n netmanager-isolir create secret generic netmanager-isolir-token \
  --from-literal=NETMANAGER_TOKEN="$ISOLIR_SERVICE_TOKEN"
```

Hapus `tls.key` dari mesin lokal setelah Secret dibuat. Bila paket image di GHCR
privat, buat juga Secret `netmanager-isolir-registry` (tipe docker-registry).

## Deploy

Image dibangun oleh `build-image.yml` pada push ke `main` dan diberi tag
`<12 karakter commit>-<epoch commit>`. Isi placeholder `{{ISOLIR_IMAGE}}` di
manifest dengan image itu, lalu apply:

```sh
sed "s|{{ISOLIR_IMAGE}}|ghcr.io/hadiahterlupakan/netmanager-isolir:<tag>|g" \
  deploy/k8s/isolir.yaml | kubectl apply -f -
```

Pod dipaku ke node `radpro`, dan Service memakai `externalTrafficPolicy: Local`
agar IP sumber router terbaca apa adanya oleh pembatas per IP.

## Peringatan HTTPS

Sertifikatnya self-signed dan tidak mungkin cocok dengan situs yang dituju
pelanggan, jadi browser dan aplikasi menampilkan peringatan sertifikat atau
menolak koneksi (HSTS, certificate pinning). Itu batas yang tidak bisa
dihindari pada HTTPS; halaman isolir hanya terlihat penuh lewat HTTP atau bila
pelanggan melewati peringatan. Karena itu galat handshake TLS sengaja tidak
dicatat di log.
