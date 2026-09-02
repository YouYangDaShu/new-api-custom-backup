import fs from 'node:fs/promises'
import path from 'node:path'
const dir = path.resolve('src/i18n/locales')
const newKeys = {
  en: {'抽奖系统':'Lottery','抽奖管理':'Lottery Management','每消费达到阈值可获得一次抽奖机会':'Earn one draw each time you reach the consumption threshold','状态':'Status','已启用':'Enabled','未启用':'Disabled','累计进度':'Progress','可用抽奖次数':'Available draws','立即抽奖':'Draw now','处理中':'Processing','抽奖结果':'Result','暂时无法加载抽奖信息':'Unable to load lottery information','启用抽奖':'Enable lottery','消费阈值':'Consumption threshold','奖品列表':'Prize list','保存奖品列表':'Save prize list','重置所有用户进度':'Reset all user progress','用户 ID':'User ID','重置指定用户':'Reset specified user','用户进度':'User progress','Loading':'Loading'},
  zh: {'抽奖系统':'抽奖系统','抽奖管理':'抽奖管理','每消费达到阈值可获得一次抽奖机会':'每消费达到阈值可获得一次抽奖机会','状态':'状态','已启用':'已启用','未启用':'未启用','累计进度':'累计进度','可用抽奖次数':'可用抽奖次数','立即抽奖':'立即抽奖','处理中':'处理中','抽奖结果':'抽奖结果','暂时无法加载抽奖信息':'暂时无法加载抽奖信息','启用抽奖':'启用抽奖','消费阈值':'消费阈值','重置所有用户进度':'重置所有用户进度','用户 ID':'用户 ID','重置指定用户':'重置指定用户','用户进度':'用户进度','Loading':'加载中'},
  fr: {'抽奖系统':'Loterie','抽奖管理':'Gestion de la loterie','每消费达到阈值可获得一次抽奖机会':'Gagnez un tirage à chaque seuil de consommation','状态':'Statut','已启用':'Activé','未启用':'Désactivé','累计进度':'Progression','可用抽奖次数':'Tirages disponibles','立即抽奖':'Tirer maintenant','处理中':'Traitement','抽奖结果':'Résultat','暂时无法加载抽奖信息':'Impossible de charger la loterie','启用抽奖':'Activer la loterie','消费阈值':'Seuil de consommation','重置所有用户进度':'Réinitialiser tous les progrès','用户 ID':'ID utilisateur','重置指定用户':'Réinitialiser cet utilisateur','用户进度':'Progression utilisateur','Loading':'Chargement'},
  ja: {'抽奖系统':'抽選システム','抽奖管理':'抽選管理','每消费达到阈值可获得一次抽奖机会':'消費しきい値ごとに1回抽選できます','状态':'状態','已启用':'有効','未启用':'無効','累计进度':'累計進捗','可用抽奖次数':'利用可能な抽選回数','立即抽奖':'今すぐ抽選','处理中':'処理中','抽奖结果':'抽選結果','暂时无法加载抽奖信息':'抽選情報を読み込めません','启用抽奖':'抽選を有効化','消费阈值':'消費しきい値','重置所有用户进度':'全ユーザーの進捗をリセット','用户 ID':'ユーザーID','重置指定用户':'指定ユーザーをリセット','用户进度':'ユーザー進捗','Loading':'読み込み中'},
  ru: {'抽奖系统':'Лотерея','抽奖管理':'Управление лотереей','每消费达到阈值可获得一次抽奖机会':'Один розыгрыш за каждый порог потребления','状态':'Статус','已启用':'Включено','未启用':'Выключено','累计进度':'Прогресс','可用抽奖次数':'Доступные розыгрыши','立即抽奖':'Разыграть сейчас','处理中':'Обработка','抽奖结果':'Результат','暂时无法加载抽奖信息':'Не удалось загрузить лотерею','启用抽奖':'Включить лотерею','消费阈值':'Порог потребления','重置所有用户进度':'Сбросить прогресс всех пользователей','用户 ID':'ID пользователя','重置指定用户':'Сбросить пользователя','用户进度':'Прогресс пользователей','Loading':'Загрузка'},
  vi: {'抽奖系统':'Xổ số','抽奖管理':'Quản lý xổ số','每消费达到阈值可获得一次抽奖机会':'Nhận một lượt quay khi đạt ngưỡng chi tiêu','状态':'Trạng thái','已启用':'Đã bật','未启用':'Đã tắt','累计进度':'Tiến độ','可用抽奖次数':'Lượt quay còn lại','立即抽奖':'Quay ngay','处理中':'Đang xử lý','抽奖结果':'Kết quả','暂时无法加载抽奖信息':'Không thể tải thông tin xổ số','启用抽奖':'Bật xổ số','消费阈值':'Ngưỡng chi tiêu','重置所有用户进度':'Đặt lại tiến độ tất cả người dùng','用户 ID':'ID người dùng','重置指定用户':'Đặt lại người dùng','用户进度':'Tiến độ người dùng','Loading':'Đang tải'}
}
newKeys.zh['奖品列表']='奖品列表'; newKeys.zh['保存奖品列表']='保存奖品列表'
newKeys.fr['奖品列表']='Liste des prix'; newKeys.fr['保存奖品列表']='Enregistrer la liste des prix'
newKeys.ja['奖品列表']='賞品リスト'; newKeys.ja['保存奖品列表']='賞品リストを保存'
newKeys.ru['奖品列表']='Список призов'; newKeys.ru['保存奖品列表']='Сохранить список призов'
newKeys.vi['奖品列表']='Danh sách giải thưởng'; newKeys.vi['保存奖品列表']='Lưu danh sách giải thưởng'
const featureKeys = {
  'Channel monitoring': 'Channel monitoring', 'Channel monitor management': 'Channel monitor management', 'Live model availability by group': 'Live model availability by group', 'Unable to load channel monitors': 'Unable to load channel monitors', 'Check now': 'Check now', 'Token rankings': 'Token rankings', 'Token ranking management': 'Token ranking management', 'Enable token rankings': 'Enable token rankings', 'Enable channel monitoring': 'Enable channel monitoring', 'Unable to update channel monitor setting': 'Unable to update channel monitor setting', 'Weekly rewards by rank': 'Weekly rewards by rank', 'Reward history': 'Reward history', 'Save': 'Save', 'Display name': 'Display name', 'Public group': 'Public group', 'Primary model': 'Primary model', 'Extra models': 'Extra models', 'Interval seconds': 'Interval seconds', 'Enabled': 'Enabled', 'Disabled': 'Disabled', 'Create': 'Create', 'Edit': 'Edit', 'Delete': 'Delete', 'Cancel': 'Cancel', 'Unable to save channel monitor': 'Unable to save channel monitor', 'Unable to delete channel monitor': 'Unable to delete channel monitor', 'Unable to run channel monitor': 'Unable to run channel monitor', 'Please complete all required fields': 'Please complete all required fields', 'Unable to load token rankings': 'Unable to load token rankings', 'Invalid reward values': 'Invalid reward values', 'Unable to save token rankings': 'Unable to save token rankings', 'Week': 'Week', 'Rank': 'Rank', 'Tokens': 'Tokens', 'Reward': 'Reward', 'Available': 'Available', 'Unavailable': 'Unavailable', 'Unknown': 'Unknown', 'Recent status history': 'Recent status history', 'No history': 'No history', 'Last checked': 'Last checked', 'Time': 'Time', 'Consumption progress': 'Consumption progress', 'Welfare lottery': 'Welfare lottery'
}
for (const locale of Object.keys(newKeys)) Object.assign(newKeys[locale], featureKeys)
Object.assign(newKeys.zh, {'Channel monitoring':'渠道监控','Channel monitor management':'渠道监控管理','Live model availability by group':'按分组查看模型可用性','Unable to load channel monitors':'无法加载渠道监控','Check now':'立即检查','Token rankings':'Token 排行榜','Token ranking management':'Token 排行榜管理','Enable token rankings':'启用 Token 排行榜','Weekly rewards by rank':'按排名设置周榜奖励','Reward history':'奖励发放历史','Save':'保存'})
Object.assign(newKeys.fr, {'Channel monitoring':'Surveillance des canaux','Channel monitor management':'Gestion de la surveillance','Live model availability by group':'Disponibilité des modèles par groupe','Unable to load channel monitors':'Impossible de charger les moniteurs','Check now':'Vérifier maintenant','Token rankings':'Classements de tokens','Token ranking management':'Gestion des classements de tokens','Enable token rankings':'Activer les classements de tokens','Weekly rewards by rank':'Récompenses hebdomadaires par rang','Reward history':'Historique des récompenses','Save':'Enregistrer'})
Object.assign(newKeys.ja, {'Channel monitoring':'チャネル監視','Channel monitor management':'チャネル監視管理','Live model availability by group':'グループ別モデル可用性','Unable to load channel monitors':'チャネル監視を読み込めません','Check now':'今すぐ確認','Token rankings':'トークンランキング','Token ranking management':'トークンランキング管理','Enable token rankings':'トークンランキングを有効化','Weekly rewards by rank':'順位別週間報酬','Reward history':'報酬履歴','Save':'保存'})
Object.assign(newKeys.ru, {'Channel monitoring':'Мониторинг каналов','Channel monitor management':'Управление мониторингом каналов','Live model availability by group':'Доступность моделей по группам','Unable to load channel monitors':'Не удалось загрузить мониторы','Check now':'Проверить сейчас','Token rankings':'Рейтинг токенов','Token ranking management':'Управление рейтингом токенов','Enable token rankings':'Включить рейтинг токенов','Weekly rewards by rank':'Еженедельные награды по рангу','Reward history':'История наград','Save':'Сохранить'})
Object.assign(newKeys.vi, {'Channel monitoring':'Giám sát kênh','Channel monitor management':'Quản lý giám sát kênh','Live model availability by group':'Tính khả dụng mô hình theo nhóm','Unable to load channel monitors':'Không thể tải trình giám sát','Check now':'Kiểm tra ngay','Token rankings':'Bảng xếp hạng token','Token ranking management':'Quản lý bảng xếp hạng token','Enable token rankings':'Bật bảng xếp hạng token','Weekly rewards by rank':'Phần thưởng hàng tuần theo hạng','Reward history':'Lịch sử phần thưởng','Save':'Lưu'})
Object.assign(newKeys.zh, {'Consumption progress':'消费进度','Welfare lottery':'福利抽奖'})
Object.assign(newKeys.fr, {'Consumption progress':'Progression de consommation','Welfare lottery':'Loterie avantageuse'})
Object.assign(newKeys.ja, {'Consumption progress':'消費進捗','Welfare lottery':'福利抽選'})
Object.assign(newKeys.ru, {'Consumption progress':'Прогресс расходов','Welfare lottery':'Благотворительная лотерея'})
Object.assign(newKeys.vi, {'Consumption progress':'Tiến độ chi tiêu','Welfare lottery':'Xổ số phúc lợi'})
Object.assign(newKeys.en, {'Context over 272K':'Context over 272K'})
Object.assign(newKeys.zh, {'Context over 272K':'超 272K 上下文'})
Object.assign(newKeys.fr, {'Context over 272K':'Contexte supérieur à 272K'})
Object.assign(newKeys.ja, {'Context over 272K':'272K超のコンテキスト'})
Object.assign(newKeys.ru, {'Context over 272K':'Контекст свыше 272K'})
Object.assign(newKeys.vi, {'Context over 272K':'Ngữ cảnh trên 272K'})
Object.assign(newKeys.en, {'GPT context policy':'GPT context policy','Standard pricing':'Standard pricing','Tiered pricing (GPT long context)':'Tiered pricing (GPT long context)'})
Object.assign(newKeys.zh, {'GPT context policy':'GPT 上下文策略','Standard pricing':'标准收费','Tiered pricing (GPT long context)':'阶梯价收费（GPT 长上下文）'})
Object.assign(newKeys.fr, {'GPT context policy':'Politique de contexte GPT','Standard pricing':'Tarification standard','Tiered pricing (GPT long context)':'Tarification progressive (contexte long GPT)'})
Object.assign(newKeys.ja, {'GPT context policy':'GPTコンテキストポリシー','Standard pricing':'標準料金','Tiered pricing (GPT long context)':'段階料金（GPT長文コンテキスト）'})
Object.assign(newKeys.ru, {'GPT context policy':'Политика контекста GPT','Standard pricing':'Стандартная тарификация','Tiered pricing (GPT long context)':'Тарификация по уровням (длинный контекст GPT)'})
Object.assign(newKeys.vi, {'GPT context policy':'Chính sách ngữ cảnh GPT','Standard pricing':'Giá tiêu chuẩn','Tiered pricing (GPT long context)':'Giá theo bậc (ngữ cảnh dài GPT)'})
newKeys['zh-TW'] = {
  'GPT context policy': 'GPT 上下文策略',
  'Standard pricing': '標準收費',
  'Tiered pricing (GPT long context)': '階梯價收費（GPT 長上下文）',
  'Reject over 272K': '超過 272K 時拒絕',
}
const obsoleteKeys = [
  'Applies only to GPT text Chat/Responses requests',
  "For GPT-5.4/GPT-5.5 text models. Long context follows the model's configured pricing; tiered pricing requires an expression with len. Reject mode blocks requests over 272K.",
  'GPT context mode',
  'GPT long context',
]
for (const [locale, entries] of Object.entries(newKeys)) {
  const file = path.join(dir, `${locale}.json`)
  const json = JSON.parse(await fs.readFile(file, 'utf8'))
  Object.assign(json.translation, entries)
  for (const key of obsoleteKeys) delete json.translation[key]
  json.translation = Object.fromEntries(Object.entries(json.translation).sort(([a],[b]) => a.localeCompare(b)))
  await fs.writeFile(file, JSON.stringify(json, null, 2) + '\n')
}
