#!/usr/bin/env python3
"""把角色库里缺的头像提示词补齐。只描述外形与气质，不提作品名。"""
import json

PATH = 'scripts/avatar-prompts.json'
have = json.load(open(PATH, encoding='utf-8'))

ADD = {
    "结衣": "a playful young woman with short dark hair and a bright grin, red and white outfit",
    "椎名真白": "a quiet young woman with very long pale blonde hair, blank gentle expression, white dress",
    "青山七海": "an earnest young woman with short brown hair and a hair clip, determined expression, school blazer",
    "加藤惠": "a calm young woman with shoulder-length brown hair and a plain hairpin, neutral gentle expression, beige cardigan",
    "泽村英梨梨": "a blonde young woman with long twin tails and blue eyes, flustered proud expression, school uniform",
    "霞之丘诗羽": "an elegant young woman with long dark hair and a red headband, composed teasing smile, dark uniform",
    "千反田爱瑠": "a polite young woman with long black hair and a purple hairband, curious bright eyes, school uniform",
    "伊原摩耶花": "a frank young woman with short brown hair, direct expression, green cardigan",
    "中野三玖": "a quiet young woman with shoulder-length brown hair and headphones around her neck, shy downcast gaze",
    "中野二乃": "a strong-willed young woman with long pink hair and a ribbon, confident sharp expression",
    "中野五月": "an earnest young woman with long red hair and a star hairpin, serious expression",
    "四宫辉夜": "an elegant young woman with long black hair and a black ribbon, proud composed expression, dark formal dress",
    "藤原千花": "a cheerful young woman with short pink hair and a small bow, playful bright smile, school uniform",
    "早坂爱": "a cool young woman with long blonde hair tied back, calm sharp expression, dark maid-style outfit",
    "后藤一里": "a shy young woman with long pink hair and a blue and yellow hair clip, nervous expression, pink tracksuit",
    "伊地知虹夏": "an energetic young woman with a blonde side ponytail and a large ribbon, warm encouraging smile",
    "山田凉": "an aloof young woman with short blue hair, indifferent expression, dark hoodie",
    "喜多郁代": "a sunny young woman with short red hair and a bright smile, casual bright jacket",
    "平泽唯": "a laid-back young woman with shoulder-length brown hair and a yellow hair clip, sleepy smile, school uniform",
    "秋山澪": "a serious young woman with long black hair, shy expression, school uniform",
    "田井中律": "a lively young woman with short brown hair and a yellow headband, mischievous grin",
    "御坂美琴": "a determined young woman with short chestnut hair, bright confident expression, school uniform",
    "白井黑子": "a petite young woman with twin-tail brown hair and red ribbons, exaggerated smile, school uniform",
    "佐天泪子": "a cheerful young woman with long black hair and a flower hairpin, relaxed smile",
    "凉宫春日": "an energetic young woman with short brown hair and a yellow ribbon, forceful confident expression",
    "长门有希": "a quiet young woman with short purple hair and glasses, expressionless face, school uniform",
    "古河渚": "a gentle young woman with short brown hair and a small hair clip, soft timid smile",
    "藤林杏": "a strong-willed young woman with long purple hair and a red ribbon, sharp expression",
    "逢坂大河": "a petite young woman with long light brown hair, fierce pout, school uniform",
    "栉枝实乃梨": "a cheerful young woman with short red hair and a bright grin, energetic pose",
    "高坂桐乃": "a stylish young woman with long blonde hair, proud annoyed expression, casual fashion",
    "黑猫": "a young woman with long black hair and a black cat-ear headband, serious gothic outfit",
    "时崎狂三": "an elegant young woman with long black hair in twin tails and one red eye, mysterious smile, dark gothic dress",
    "夜刀神十香": "a straightforward young woman with very long dark purple hair and a purple ribbon, bright open expression",
    "战场原黑仪": "an elegant young woman with long black hair, cool sharp gaze, dark school uniform",
    "八九寺真宵": "a small cheerful young girl with twin-tail black hair and a large backpack, playful expression",
    "小野寺小咲": "a gentle young woman with short brown hair and a flower hairpin, shy smile",
    "桐崎千棘": "a strong-willed young woman with long blonde hair and a red ribbon, defiant expression",
    "宫园薰": "a lively young woman with long blonde hair, bright carefree smile, light dress",
    "夏娜": "a petite young woman with very long black hair and a red ribbon, fierce determined expression, dark cloak",
    "神尾观铃": "a gentle young girl with long blonde hair and a white hairband, soft absent smile",
    "春日野穹": "a quiet young woman with very long silver hair and a black hairband, reserved expression, white dress",
    "立华奏": "a calm young woman with long silver hair, blank gentle expression, white school uniform",
    "千石抚子": "a shy young woman with short dark hair and a large hat, timid downcast gaze",
    "羽川翼": "a composed young woman with long black hair in twin braids and glasses, thoughtful polite smile",
    "雪之下阳乃": "a confident young woman with long black hair, playful knowing smile, elegant coat",
    "平冢静": "a blunt woman in her early thirties with long black hair, tired sharp expression, white lab coat",
}

for k, v in ADD.items():
    have.setdefault(k, v)

json.dump(have, open(PATH, 'w', encoding='utf-8'), ensure_ascii=False, indent=1)
print('total entries:', len([k for k in have if not k.startswith('_')]))
