"""
API de loterias.

Espelha o fluxo em etapas do MiroFish: dispara um estudo em background,
acompanha o progresso e devolve o boletim ao final. Reaproveita o
`TaskManager` ja usado pela construcao de grafo.
"""

import threading
from typing import Any, Dict

from flask import jsonify, request

from . import lottery_bp
from ..lottery.analyzer import FeatureSet, analyze
from ..lottery.catalog import LOTTERIES, get_lottery
from ..lottery import combinatorics as cb
from ..lottery.data_source import HistoryStore
from ..lottery.economics import expected_value
from ..lottery.engine import LotteryEngine, LotteryRunManager, RunConfig
from ..lottery.popularity import fit_popularity
from ..lottery.wheeling import build_wheel, certify
from ..lottery.worlds import WORLD_REGISTRY
from ..models.task import TaskManager, TaskStatus
from ..utils.logger import get_logger

logger = get_logger('mirofish.api.lottery')
task_manager = TaskManager()


def _error(message: str, status: int = 400):
    return jsonify({"success": False, "error": message}), status


def _ok(payload: Dict[str, Any], status: int = 200):
    body = {"success": True}
    body.update(payload)
    return jsonify(body), status


# --------------------------------------------------------------------- catalogo

@lottery_bp.route('/modalidades', methods=['GET'])
def list_lotteries():
    """Modalidades disponiveis e suas propriedades combinatorias."""
    return _ok({
        "modalidades": [
            {
                "slug": lot.slug,
                "nome": lot.name,
                "universo": lot.total_numbers,
                "dezenas_sorteadas": lot.picks,
                "aposta_minima": lot.min_bet_size,
                "aposta_maxima": lot.max_bet_size,
                "preco_aposta": lot.base_price,
                "combinacoes": lot.total_combinations,
                "enumeravel": lot.enumerable,
                "faixas_premiadas": lot.prize_tiers,
            }
            for lot in LOTTERIES.values()
        ]
    })


@lottery_bp.route('/mundos', methods=['GET'])
def list_worlds():
    """Mundos paralelos disponiveis, com hipotese e ressalva de cada um."""
    return _ok({"mundos": [w.to_dict() for w in WORLD_REGISTRY.values()]})


# ------------------------------------------------------------------------ dados

@lottery_bp.route('/historico/<slug>', methods=['GET'])
def history_summary(slug: str):
    """Resumo do historico em cache e ultimos concursos."""
    try:
        lottery = get_lottery(slug)
    except ValueError as exc:
        return _error(str(exc), 404)

    draws = HistoryStore(lottery).load()
    if not draws:
        return _ok({
            "modalidade": slug,
            "concursos": 0,
            "aviso": "Nenhum resultado em cache. Rode POST /api/lottery/sync primeiro.",
            "ultimos": [],
        })

    limit = min(int(request.args.get('limit', 10)), 100)
    return _ok({
        "modalidade": slug,
        "concursos": len(draws),
        "primeiro": draws[0].number,
        "ultimo": draws[-1].number,
        "data_ultimo": draws[-1].date,
        "ultimos": [
            {
                "concurso": d.number,
                "data": d.date,
                "dezenas": list(d.numbers),
                "arrecadacao": d.revenue,
                "premios": {str(h): t.to_dict() for h, t in sorted(d.prizes.items(), reverse=True)},
            }
            for d in draws[-limit:][::-1]
        ],
    })


@lottery_bp.route('/sync', methods=['POST'])
def sync_history():
    """Baixa da Caixa os concursos que faltam no cache (em background)."""
    data = request.get_json(silent=True) or {}
    slug = data.get('modalidade', 'lotofacil')

    try:
        lottery = get_lottery(slug)
    except ValueError as exc:
        return _error(str(exc), 404)

    task_id = task_manager.create_task('lottery_sync', {"modalidade": slug})

    def _worker():
        try:
            task_manager.update_task(task_id, status=TaskStatus.PROCESSING, message="sincronizando")
            store = HistoryStore(lottery)

            def _progress(done: int, total: int):
                pct = int(100 * done / max(total, 1))
                task_manager.update_task(
                    task_id, progress=pct, message=f"{done}/{total} concursos"
                )

            stats = store.sync(progress=_progress)
            task_manager.complete_task(task_id, stats)
        except Exception as exc:
            logger.exception("Falha ao sincronizar %s", slug)
            task_manager.fail_task(task_id, str(exc))

    threading.Thread(target=_worker, daemon=True).start()
    return _ok({"task_id": task_id, "modalidade": slug}, 202)


@lottery_bp.route('/task/<task_id>', methods=['GET'])
def task_status(task_id: str):
    """Progresso de uma tarefa (sync ou estudo)."""
    task = task_manager.get_task(task_id)
    if not task:
        return _error("Tarefa não encontrada", 404)
    return _ok({"task": task.to_dict()})


# ---------------------------------------------------------------------- analise

@lottery_bp.route('/analise/<slug>', methods=['GET'])
def analysis(slug: str):
    """Estatisticas do historico, modelo de popularidade e valor esperado."""
    try:
        lottery = get_lottery(slug)
    except ValueError as exc:
        return _error(str(exc), 404)

    draws = HistoryStore(lottery).load()
    if not draws:
        return _error("Sem histórico em cache. Rode POST /api/lottery/sync.", 409)

    window = request.args.get('janela', type=int)
    if window:
        draws = draws[-window:]

    features = FeatureSet(lottery)
    return _ok({
        "analise": analyze(draws, lottery, features).to_dict(),
        "popularidade": fit_popularity(draws, lottery, features).to_dict(),
        "valor_esperado": expected_value(lottery, draws).to_dict(),
    })


# ----------------------------------------------------------------------- estudo

@lottery_bp.route('/estudo', methods=['POST'])
def start_study():
    """Dispara um estudo completo em background."""
    data = request.get_json(silent=True) or {}

    try:
        config = RunConfig(
            lottery=data.get('modalidade', 'lotofacil'),
            n_games=int(data.get('n_jogos', 8)),
            worlds=data.get('mundos') or list(WORLD_REGISTRY),
            portfolio_mode=data.get('modo', 'valor_esperado'),
            run_backtest=bool(data.get('rodar_backtest', True)),
            backtest_draws=int(data.get('backtest_concursos', 60)),
            backtest_games=int(data.get('backtest_jogos', 5)),
            wheel_base_size=int(data.get('fechamento_dezenas', 18)),
            wheel_target_c=data.get('fechamento_alvo_c'),
            wheel_target_hits=data.get('fechamento_alvo_pontos'),
            sync_before_run=bool(data.get('sincronizar', True)),
            seed=int(data.get('seed', 2024)),
        )
    except (TypeError, ValueError) as exc:
        return _error(f"Parâmetros inválidos: {exc}")

    errors = config.validate()
    if errors:
        return _error("; ".join(errors))

    unknown = [w for w in config.worlds if w not in WORLD_REGISTRY]
    if unknown:
        return _error(f"Mundos desconhecidos: {', '.join(unknown)}")

    try:
        get_lottery(config.lottery)
    except ValueError as exc:
        return _error(str(exc), 404)

    run_id = LotteryRunManager.new_id()
    task_id = task_manager.create_task('lottery_study', {"run_id": run_id})

    def _worker():
        try:
            task_manager.update_task(task_id, status=TaskStatus.PROCESSING, message="iniciando")
            engine = LotteryEngine(config)

            def _on_progress(progress):
                task_manager.update_task(
                    task_id,
                    progress=progress.percent,
                    message=progress.message,
                    progress_detail=progress.to_dict(),
                )

            engine.set_progress_callback(_on_progress)
            result = engine.run()
            result["run_id"] = run_id
            result["status"] = "concluido"
            LotteryRunManager.save(run_id, result)
            task_manager.complete_task(task_id, {"run_id": run_id})
        except Exception as exc:
            logger.exception("Falha no estudo %s", run_id)
            task_manager.fail_task(task_id, str(exc))

    threading.Thread(target=_worker, daemon=True).start()
    return _ok({"run_id": run_id, "task_id": task_id, "config": config.to_dict()}, 202)


@lottery_bp.route('/estudo/<run_id>', methods=['GET'])
def get_study(run_id: str):
    """Boletim completo de um estudo concluido."""
    data = LotteryRunManager.get(run_id)
    if not data:
        return _error("Estudo não encontrado (pode ainda estar rodando)", 404)
    return _ok({"estudo": data})


@lottery_bp.route('/estudos', methods=['GET'])
def list_studies():
    """Estudos ja executados."""
    limit = min(int(request.args.get('limit', 50)), 200)
    return _ok({"estudos": LotteryRunManager.list(limit)})


@lottery_bp.route('/estudo/<run_id>', methods=['DELETE'])
def delete_study(run_id: str):
    if not LotteryRunManager.delete(run_id):
        return _error("Estudo não encontrado", 404)
    return _ok({"run_id": run_id, "removido": True})


# ------------------------------------------------------------------ ferramentas

@lottery_bp.route('/fechamento', methods=['POST'])
def wheel_endpoint():
    """Constroi um fechamento sob medida e certifica a garantia exata."""
    data = request.get_json(silent=True) or {}
    slug = data.get('modalidade', 'lotofacil')

    try:
        lottery = get_lottery(slug)
    except ValueError as exc:
        return _error(str(exc), 404)

    base_numbers = data.get('dezenas')
    if not base_numbers or not isinstance(base_numbers, list):
        return _error("Informe 'dezenas': a lista do conjunto base")

    try:
        base_numbers = sorted({int(n) for n in base_numbers})
    except (TypeError, ValueError):
        return _error("'dezenas' deve conter apenas números inteiros")

    if any(n < 1 or n > lottery.total_numbers for n in base_numbers):
        return _error(f"dezenas devem estar entre 1 e {lottery.total_numbers}")

    n_games = int(data.get('n_jogos', 10))
    if n_games < 1 or n_games > 500:
        return _error("n_jogos deve estar entre 1 e 500")

    if not lottery.enumerable:
        return _error(
            f"{lottery.name} tem {lottery.total_combinations:,} combinações — "
            f"a certificação exaustiva não é viável nesta modalidade."
        )

    try:
        wheel = build_wheel(
            lottery,
            base_numbers,
            n_games=n_games,
            target_c=data.get('alvo_c'),
            target_hits=data.get('alvo_pontos'),
        )
    except ValueError as exc:
        return _error(str(exc))

    space = cb.enumerate_masks(lottery.total_numbers, lottery.picks)
    certificate = certify(lottery, wheel.games, base_numbers, space)

    return _ok({"fechamento": wheel.to_dict(), "certificado": certificate.to_dict()})


@lottery_bp.route('/conferir', methods=['POST'])
def check_games():
    """Confere jogos contra um concurso ja realizado."""
    data = request.get_json(silent=True) or {}
    slug = data.get('modalidade', 'lotofacil')

    try:
        lottery = get_lottery(slug)
    except ValueError as exc:
        return _error(str(exc), 404)

    games = data.get('jogos')
    if not games or not isinstance(games, list):
        return _error("Informe 'jogos': lista de listas de dezenas")

    draws = HistoryStore(lottery).load()
    if not draws:
        return _error("Sem histórico em cache. Rode POST /api/lottery/sync.", 409)

    concurso = data.get('concurso')
    if concurso is None:
        draw = draws[-1]
    else:
        match = [d for d in draws if d.number == int(concurso)]
        if not match:
            return _error(f"Concurso {concurso} não encontrado no cache", 404)
        draw = match[0]

    drawn = set(draw.numbers)
    results = []
    total_prize = 0.0

    for game in games:
        try:
            numbers = sorted({int(n) for n in game})
        except (TypeError, ValueError):
            return _error("Cada jogo deve conter apenas números inteiros")

        matched = sorted(drawn & set(numbers))
        hits = len(matched)
        tier = draw.prizes.get(hits)
        prize = tier.value if tier and hits in lottery.prize_tiers else 0.0
        total_prize += prize
        results.append({
            "dezenas": numbers,
            "acertos": hits,
            "acertadas": matched,
            "premio": round(prize, 2),
        })

    cost = len(results) * lottery.base_price
    return _ok({
        "concurso": draw.number,
        "data": draw.date,
        "sorteio": list(draw.numbers),
        "resultados": results,
        "custo_total": round(cost, 2),
        "premio_total": round(total_prize, 2),
        "resultado_liquido": round(total_prize - cost, 2),
    })
