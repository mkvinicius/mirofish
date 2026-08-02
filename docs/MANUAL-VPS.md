# Manual de instalação e uso — MiroFish Loterias (para leigos)

Este guia leva o sistema do zero até funcionando no seu VPS Hostinger KVM 4,
com senha de acesso e alarme diário de oportunidades. Não é preciso saber
programar: **copie e cole cada bloco de comandos, na ordem, e confira o
"você deve ver"** de cada etapa.

> Tempo total estimado: 30–40 minutos.

---

## Parte 1 — Preparar o VPS

### 1.1 Criar o servidor

No painel da Hostinger, ao configurar o VPS, escolha o sistema operacional
**Ubuntu 24.04 LTS** (sem painel/template adicional). Anote o **endereço IP**
e a **senha de root** que a Hostinger mostrar.

### 1.2 Entrar no servidor

No painel da Hostinger há um botão **"Terminal do navegador"** — é o caminho
mais fácil. (Alternativa: no Windows, abra o PowerShell e digite
`ssh root@SEU_IP`, respondendo `yes` e colando a senha.)

Você deve ver algo como `root@srv123456:~#`. Tudo daqui em diante é digitado
nessa tela.

### 1.3 Instalar os programas necessários

```bash
apt update && apt upgrade -y
apt install -y git python3 python3-venv python3-pip nginx apache2-utils ufw curl
curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
apt install -y nodejs
```

**Você deve ver:** várias linhas de instalação sem mensagens vermelhas de erro
no final. Confira as versões:

```bash
python3 --version   # deve mostrar 3.11 ou superior
node --version      # deve mostrar v20.x
```

### 1.4 Ligar o firewall (proteção básica)

```bash
ufw allow OpenSSH
ufw allow 80/tcp
ufw --force enable
```

**Você deve ver:** `Firewall is active`. Isso deixa abertas só as portas do
terminal (SSH) e do site (80). A porta interna do sistema (5001) fica
inacessível de fora — é o comportamento correto.

---

## Parte 2 — Instalar o sistema

### 2.1 Baixar o projeto

```bash
cd /opt
git clone -b claude/lottery-parallel-worlds-analysis-mv9bx0 https://github.com/mkvinicius/mirofish.git
cd /opt/mirofish
```

> Se o repositório for privado, o GitHub vai pedir usuário e um *personal
> access token* como senha (crie em github.com → Settings → Developer
> settings → Tokens).

### 2.2 Configurar o modo loteria

```bash
cp .env.example .env
sed -i 's/^MIROFISH_MODE=full/MIROFISH_MODE=loteria/' .env
```

Pronto — no modo loteria **não é preciso preencher nenhuma chave de API**
(as linhas de LLM/ZEP do arquivo podem ficar como estão, são ignoradas).

### 2.3 Instalar o backend (o motor)

```bash
cd /opt/mirofish/backend
python3 -m venv .venv
.venv/bin/pip install -r requirements-loteria.txt
```

**Você deve ver:** `Successfully installed ...` no final (demora 1–2 min).

Teste rápido do motor:

```bash
MIROFISH_MODE=loteria .venv/bin/python -c "from app import create_app; create_app(); print('MOTOR OK')"
```

**Você deve ver:** `MOTOR OK`.

### 2.4 Instalar o frontend (as telas)

```bash
cd /opt/mirofish/frontend
npm ci
npm run build
```

**Você deve ver:** `✓ built in ...s` no final. As telas prontas ficam na
pasta `dist/`.

---

## Parte 3 — Colocar no ar (com senha)

### 3.1 Criar sua senha de acesso

Troque `SEU_USUARIO` pelo nome que quiser usar no login:

```bash
htpasswd -c /etc/nginx/.htpasswd SEU_USUARIO
```

Digite a senha duas vezes (nada aparece enquanto digita — é normal).

### 3.2 Ativar o site

```bash
cp /opt/mirofish/deploy/nginx-loteria.conf /etc/nginx/sites-available/mirofish
ln -sf /etc/nginx/sites-available/mirofish /etc/nginx/sites-enabled/mirofish
rm -f /etc/nginx/sites-enabled/default
nginx -t && systemctl reload nginx
```

**Você deve ver:** `syntax is ok` e `test is successful`.

### 3.3 Ligar o motor como serviço permanente

```bash
cp /opt/mirofish/deploy/mirofish-loteria.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now mirofish-loteria
systemctl status mirofish-loteria --no-pager | head -5
```

**Você deve ver:** `Active: active (running)`. A partir daqui o motor liga
sozinho mesmo se o servidor reiniciar.

### 3.4 Acessar

Abra o navegador no seu computador ou celular e digite:

```
http://SEU_IP/loteria
```

O navegador pedirá o usuário e a senha da etapa 3.1. Depois disso, a tela
das loterias abre. **Instalação concluída.**

### 3.5 Ligar o alarme diário de oportunidades

```bash
(crontab -l 2>/dev/null; echo "0 12 * * * cd /opt/mirofish/backend && .venv/bin/python scripts/alerta_oportunidades.py >> data/alertas_cron.log 2>&1") | crontab -
```

Todo dia às 9h (horário de Brasília) o sistema confere as três loterias e
registra em `backend/data/alertas.log` quando alguma cruzar 70% de retorno.

**Opcional — receber o alerta no celular (Telegram):**
1. No Telegram, fale com o `@BotFather`, mande `/newbot` e guarde o token.
2. Fale com o `@userinfobot` para descobrir seu `chat id`.
3. Adicione as duas linhas ao arquivo de configuração:
   ```bash
   echo "TELEGRAM_BOT_TOKEN=cole_o_token_aqui" >> /opt/mirofish/.env
   echo "TELEGRAM_CHAT_ID=cole_o_id_aqui" >> /opt/mirofish/.env
   ```
4. Mande um "oi" para o seu bot no Telegram (isso autoriza o envio) e teste:
   ```bash
   cd /opt/mirofish/backend && .venv/bin/python scripts/alerta_oportunidades.py
   ```

---

## Parte 4 — O que fazer em cada tela

### 4.1 O painel "Onde vale apostar hoje" (topo)

É a **primeira coisa a olhar, sempre**. Mostra o próximo concurso de cada
loteria e o *retorno esperado*: de cada R$ 1 apostado, quanto volta em média.

- **Abaixo de 70%** (o normal): dia caro. A decisão racional é não jogar.
- **70–100%**: acumulação relevante — o alarme te avisa. Jogo "menos caro".
- **Acima de 100%** (raro, alerta forte): o único momento em que a matemática
  para de cobrar pedágio. Se você joga, é o dia.

A chance de acertar **nunca muda** — o que muda é o tamanho do prêmio em
relação ao preço do bilhete.

### 4.2 Painel de configuração (coluna da esquerda)

| Campo | O que escolher |
|---|---|
| **Modalidade** | A loteria do estudo. Comece pela Lotofácil (estudo em ~35 s; Quina ~4 min; Mega ~7 min) |
| **Quantos jogos** | Seu orçamento. 8–10 jogos é um bom ponto de partida |
| **Como montar o bilhete** | Deixe em **Otimizado** (o padrão). Os outros modos servem para comparar |
| **Mundos paralelos** | Deixe todos marcados — o backtest precisa do controle |
| **Backtest** | Deixe ligado na primeira vez (é a parte educativa); pode desligar depois para o estudo sair mais rápido |

Clique em **Rodar estudo** e acompanhe a barra de progresso.

### 4.3 Boletim de apostas

**São estes os números para jogar.** Cada linha é um jogo; marque as dezenas
no volante exatamente como mostradas. Ao lado de cada jogo:

- **rateio estimado** — quanto o prêmio dividiria em relação a um jogo médio.
  `0,3×` significa: se acertar, estima-se ~3× menos gente dividindo com você.
- **retorno esperado** — o valor médio daquele jogo em reais.

### 4.4 Garantia certificada

Frases do tipo *"faz pelo menos 12 pontos em 1 a cada 9 sorteios"*. Não são
estimativas: o sistema conferiu o seu bilhete contra **todos** os sorteios
possíveis. É o que esperar do bilhete na prática, concurso após concurso.

### 4.5 Competição dos mundos (backtest)

A tabela das 8 estratégias disputando nos últimos concursos reais. **Ela
existe para te proteger**: repare que o ranking muda a cada estudo e que o
mundo "uniforme" (pura sorte) às vezes vence — é a prova, com seus próprios
dados, de que "estratégia de números" não existe. Se algum dia um vendedor
te oferecer um método milagroso, lembre desta tabela.

### 4.6 Modelo de popularidade / Valor esperado / Monitor de viés

Cartões informativos: de onde vêm os jogos anti-populares, quanto a loteria
devolve por real, e a vigilância estatística da máquina de sorteio. Não
exigem nenhuma ação — são a prestação de contas do sistema.

### 4.7 "Leia antes de apostar"

As quatro regras do jogo honesto. A mais importante: **aposte apenas o que
puder perder integralmente** — o sistema otimiza as migalhas recuperáveis,
mas a loteria continua sendo, por desenho, um jogo de perda esperada.

---

## Parte 5 — Rotina de uso sugerida

1. **Não faça nada no dia a dia.** O alarme vigia por você.
2. Quando o alarme apontar uma janela (ou em setembro/dezembro, época da
   Lotofácil da Independência e da Mega da Virada): abra o painel, rode um
   estudo **Otimizado** na modalidade da janela e jogue os jogos do boletim,
   dentro do orçamento que você definiu **antes** de abrir a tela.
3. Depois do sorteio, confira na tela de histórico — ou aceite o resultado
   mais provável (perder) com a tranquilidade de quem pagou o menor pedágio
   matematicamente possível.

---

## Parte 6 — Manutenção

| Tarefa | Comando |
|---|---|
| Ver se o motor está no ar | `systemctl status mirofish-loteria` |
| Reiniciar o motor | `systemctl restart mirofish-loteria` |
| Ver os logs do motor | `journalctl -u mirofish-loteria -n 50 --no-pager` |
| Atualizar o sistema (novo código) | `cd /opt/mirofish && git pull && cd frontend && npm ci && npm run build && systemctl restart mirofish-loteria` |
| Ver alertas registrados | `cat /opt/mirofish/backend/data/alertas.log` |
| Testar o alarme agora | `cd /opt/mirofish/backend && .venv/bin/python scripts/alerta_oportunidades.py` |

**Problemas comuns:**

- *Tela abre mas os dados não carregam* → motor parado: `systemctl restart mirofish-loteria`.
- *"502 Bad Gateway"* → mesmo caso acima; se persistir, veja `journalctl -u mirofish-loteria -n 50`.
- *Estudo da Mega falhou por memória* → não rode dois estudos grandes ao
  mesmo tempo; a Mega usa ~11 GB de RAM no pico.
- *Esqueci a senha do site* → recrie: `htpasswd -c /etc/nginx/.htpasswd SEU_USUARIO`.
