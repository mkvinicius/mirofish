"""
API路由模块

Em MIROFISH_MODE=loteria apenas o blueprint de loterias e carregado. Isso
permite rodar o motor de loterias sem instalar OASIS/torch/Zep — a diferenca
entre uma VPS de 16 GB e uma de 4 GB.
"""

from flask import Blueprint

from ..config import Config

graph_bp = Blueprint('graph', __name__)
simulation_bp = Blueprint('simulation', __name__)
report_bp = Blueprint('report', __name__)
lottery_bp = Blueprint('lottery', __name__)

from . import lottery  # noqa: E402, F401

if not Config.LOTTERY_ONLY:
    from . import graph  # noqa: E402, F401
    from . import simulation  # noqa: E402, F401
    from . import report  # noqa: E402, F401

