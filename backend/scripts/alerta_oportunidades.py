"""
Alarme diario de oportunidades — feito para rodar no cron da VPS.

Consulta o proximo concurso das tres loterias, calcula o retorno esperado e:
  - registra o retrato do dia em backend/data/oportunidades_diario.jsonl
  - se alguma modalidade cruzar o limiar (ALERTA_LIMIAR, padrao 0.70),
    grava o alerta em backend/data/alertas.log e, se TELEGRAM_BOT_TOKEN e
    TELEGRAM_CHAT_ID estiverem no .env, envia mensagem no Telegram.

Uso manual:    python scripts/alerta_oportunidades.py
No cron (9h de Brasilia = 12h UTC):
    0 12 * * * cd /opt/mirofish/backend && .venv/bin/python scripts/alerta_oportunidades.py >> data/alertas_cron.log 2>&1
"""

import json
import os
import sys
import urllib.parse
import urllib.request
from datetime import datetime, timezone

_scripts_dir = os.path.dirname(os.path.abspath(__file__))
_backend_dir = os.path.abspath(os.path.join(_scripts_dir, ".."))
sys.path.insert(0, _backend_dir)

from dotenv import load_dotenv

load_dotenv(os.path.join(_backend_dir, "..", ".env"))

from app.lottery.catalog import LOTTERIES  # noqa: E402
from app.lottery.data_source import HistoryStore  # noqa: E402
from app.lottery.opportunity import scan_opportunities  # noqa: E402

DATA_DIR = os.path.join(_backend_dir, "data")
DAILY_LOG = os.path.join(DATA_DIR, "oportunidades_diario.jsonl")
ALERT_LOG = os.path.join(DATA_DIR, "alertas.log")

THRESHOLD = float(os.environ.get("ALERTA_LIMIAR", "0.70"))
STRONG_THRESHOLD = 1.00


def _sync_quietly() -> None:
    """Busca os concursos que faltam (normalmente 0-3 por dia)."""
    for lottery in LOTTERIES.values():
        try:
            HistoryStore(lottery).sync(max_workers=4)
        except Exception as exc:  # sem rede, segue com o cache
            print(f"[aviso] sync {lottery.slug} falhou: {exc}")


def _send_telegram(text: str) -> bool:
    token = os.environ.get("TELEGRAM_BOT_TOKEN")
    chat_id = os.environ.get("TELEGRAM_CHAT_ID")
    if not token or not chat_id:
        return False
    try:
        url = f"https://api.telegram.org/bot{token}/sendMessage"
        data = urllib.parse.urlencode({"chat_id": chat_id, "text": text}).encode()
        with urllib.request.urlopen(urllib.request.Request(url, data=data), timeout=20) as resp:
            return resp.status == 200
    except Exception as exc:
        print(f"[aviso] Telegram falhou: {exc}")
        return False


def main() -> None:
    now = datetime.now(timezone.utc).isoformat(timespec="seconds")
    _sync_quietly()

    result = scan_opportunities()
    os.makedirs(DATA_DIR, exist_ok=True)

    with open(DAILY_LOG, "a", encoding="utf-8") as fh:
        fh.write(json.dumps({"quando": now, **result}, ensure_ascii=False) + "\n")

    print(f"=== Oportunidades {now} (limiar de alerta: {THRESHOLD:.0%}) ===")
    alerts = []
    for m in result["modalidades"]:
        ratio = m["retorno_esperado"]["taxa"]
        line = (
            f"{m['nome']}: concurso {m['proximo_concurso']} em {m['data']} — "
            f"retorno esperado {ratio:.1%}, bolo R$ {m['bolo_principal']:,.0f}".replace(",", ".")
        )
        print(("!! " if ratio >= THRESHOLD else "   ") + line)
        if ratio >= THRESHOLD:
            level = "OPORTUNIDADE FORTE (acima de 100%!)" if ratio >= STRONG_THRESHOLD else "atenção"
            alerts.append(f"[{level}] {line}")

    if alerts:
        message = (
            "🎯 MiroFish Loterias — alerta de oportunidade\n"
            + "\n".join(alerts)
            + "\n\nAbra o painel e rode um estudo no modo Otimizado antes de apostar. "
            "Lembrete: retorno esperado alto não é ganho garantido; use o orçamento definido."
        )
        with open(ALERT_LOG, "a", encoding="utf-8") as fh:
            fh.write(f"{now}  " + " | ".join(alerts) + "\n")
        sent = _send_telegram(message)
        print(f"[alerta registrado em {ALERT_LOG}; telegram={'enviado' if sent else 'não configurado'}]")
    else:
        print("[nenhuma modalidade cruzou o limiar hoje]")


if __name__ == "__main__":
    main()
