"use strict";
const $ = (selector, root = document) => root.querySelector(selector);
let revision = "", csrf = "", baseline = "", loaded = false, dirty = false, saving = false;
let entities = [], entitiesReady = false, nextId = 1;
const domains = new Set(["switch", "light", "fan", "input_boolean"]);
function normalize(text) {
  return text.toLowerCase().trim().replaceAll("ё", "е").replace(/[ .,!?:;…]+$/u, "").replace(/\s+/gu, " ").replace(/^алиса(?:,\s*|\s+)/u, "").trim();
}
function collect() {
  return [...document.querySelectorAll(".rule-card")].map(card => {
    const entity = $(".entity", card).value.trim();
    return {phrases: $(".phrases", card).value.split("\n").map(s => s.trim()).filter(Boolean), entity_id: entity, domain: entity.split(".")[0], service: $(".action", card).value};
  });
}
function notice(message = "", kind = "error") {
  const box = $("#notice"); box.textContent = message; box.className = `notice ${kind}`; box.hidden = !message;
}
function noun(n, one, few, many) { const v=n%100; return v>=11&&v<=14 ? many : n%10===1 ? one : n%10>=2&&n%10<=4 ? few : many; }
function updateState() {
  if (!loaded) return;
  const rules = collect(); dirty = JSON.stringify(rules) !== baseline;
  $("#rule-count").textContent = rules.length;
  $("#summary-label").textContent = noun(rules.length, "команда", "команды", "команд");
  const count = new Set(rules.map(r => r.entity_id).filter(Boolean)).size;
  $("#entity-count").textContent = `${count} ${noun(count, "сущность", "сущности", "сущностей")}`;
  $("#save-state").textContent = saving ? "Сохраняем…" : dirty ? "Есть несохранённые изменения" : "Все изменения сохранены";
  $("#save-indicator").classList.toggle("dirty", dirty);
  $("#save").disabled = saving || !dirty;
  $("#reload").disabled = saving; $("#add").disabled = saving || rules.length >= 256;
  $("#preview-button").disabled = saving;
  $("#empty").hidden = rules.length !== 0;
  $("#rules").hidden = rules.length === 0;
  $("#preview-result").hidden = true;
}
function entityHint(card) {
  const id = $(".entity", card).value.trim(), entity = entities.find(e => e.entity_id === id), hint = $(".entity-hint", card);
  hint.classList.remove("unavailable");
  if (entity) {
    const names = {on:"включено",off:"выключено",unavailable:"недоступно",unknown:"состояние неизвестно"};
    hint.textContent = `${entity.name} · ${names[entity.state] || entity.state}`;
    hint.classList.toggle("unavailable", ["unavailable", "unknown"].includes(entity.state));
  } else { hint.textContent = !id ? "Выберите из списка или введите идентификатор" : entitiesReady ? "Нет в списке HA — проверьте идентификатор" : "Идентификатор можно ввести вручную"; }
}
function addCard(rule = {phrases:[],entity_id:"",service:"turn_on"}) {
  const card = $("#rule-template").content.firstElementChild.cloneNode(true), id = nextId++;
  for (const field of ["phrase", "entity", "action"]) {
    const input = $(field === "phrase" ? ".phrases" : `.${field}`, card);
    input.id = `${field}-${id}`; $(`.${field}-label`, card).htmlFor = input.id;
  }
  $(".phrases", card).value = rule.phrases.join("\n"); $(".entity", card).value = rule.entity_id;
  $(".action", card).value = rule.service;
  card.addEventListener("input", () => { entityHint(card); updateState(); });
  card.addEventListener("change", updateState);
  $(".remove", card).addEventListener("click", () => { card.remove(); renumber(); updateState(); });
  $("#rules").append(card); entityHint(card); renumber();
  return card;
}
function renumber() { document.querySelectorAll(".rule-card").forEach((card,i) => { $(".command-number",card).textContent=`Команда ${String(i+1).padStart(2,"0")}`; $(".remove",card).setAttribute("aria-label",`Удалить команду ${i+1}`); }); }
async function api(path, options = {}) {
  const response = await fetch(path, {cache:"no-store", ...options});
  const data = await response.json();
  if (!response.ok) throw new Error(data.error || "Не удалось выполнить запрос.");
  return data;
}
async function loadRules() {
  if (dirty && !confirm("Загрузить сохранённые команды? Несохранённые изменения будут потеряны.")) return;
  $("#reload").disabled=true;
  try {
    const data=await api("/api/rules"); revision=data.revision; csrf=data.csrf_token;
    $("#rules").replaceChildren(); (data.rules || []).forEach(addCard);
    loaded=true; baseline=JSON.stringify(collect()); updateState(); notice();
    $("#rules").setAttribute("aria-busy","false");
  } catch (error) { notice(error.message); $("#save-state").textContent="Не удалось загрузить команды"; $("#reload").disabled=false; }
}
async function loadEntities() {
  const status=$("#ha-status"), button=$("#refresh-entities"); button.disabled=true;
  status.replaceChildren(); const dot=document.createElement("span");dot.className="dot loading";status.append(dot,"Получаем сущности HA…");
  try {
    const data=await api("/api/entities"); entities=data.entities;entitiesReady=true;
    const list=$("#ha-entities");list.replaceChildren();
    for (const entity of entities) { const option=document.createElement("option");option.value=entity.entity_id;option.label=`${entity.name} · ${entity.state}`;list.append(option); }
    dot.className="dot";status.replaceChildren(dot,`HA на связи · ${entities.length} ${noun(entities.length,"сущность","сущности","сущностей")}`);
  } catch (error) {dot.className="dot error";status.replaceChildren(dot,"HA не отвечает — сущности можно ввести вручную");}
  finally {button.disabled=false;document.querySelectorAll(".rule-card").forEach(entityHint);}
}
function validate(rules) {
  let valid=true;const seen=new Map();
  document.querySelectorAll(".rule-card").forEach((card,i) => {
    const rule=rules[i];let error="";
    if (!rule.phrases.length) error="Добавьте хотя бы одну фразу.";
    else if (rule.phrases.length>64 || rule.phrases.some(p=>new TextEncoder().encode(p).length>512)) error="Не больше 64 коротких фраз в одной команде.";
    else if (!domains.has(rule.domain) || !/^[a-z_]+\.[a-z0-9_]+$/u.test(rule.entity_id)) error="Укажите сущность switch, light, fan или input_boolean, например switch.cabinet.";
    for (const phrase of rule.phrases) {
      const key=normalize(phrase);
      if (!key) error="Фраза не может состоять только из обращения или знаков пунктуации.";
      if (seen.has(key)) error=`Фраза «${phrase}» уже используется в команде ${seen.get(key)+1}.`;
      else seen.set(key,i);
    }
    $(".rule-error",card).textContent=error;$(".rule-error",card).hidden=!error;
    $(".entity",card).setAttribute("aria-invalid",String(!!error));
    $(".phrases",card).setAttribute("aria-invalid",String(!!error));
    if (error) valid=false;
  });
  return valid;
}
async function save() {
  const rules=collect();if (!validate(rules)) {notice("Исправьте отмеченные команды перед сохранением.");return;}
  saving=true;$("#rules").inert=true;updateState();notice();
  try {
    const data=await api("/api/rules",{method:"PUT",headers:{"Content-Type":"application/json","X-CSRF-Token":csrf},body:JSON.stringify({revision,rules})});
    revision=data.revision;baseline=JSON.stringify(collect());notice("Команды сохранены и уже работают. Перезапуск не нужен.","success");
  } catch (error) {notice(error.message);}
  finally {saving=false;$("#rules").inert=false;updateState();}
}
function add() {if (!loaded || saving || collect().length>=256)return;const card=addCard();updateState();$(".phrases",card).focus();}
$("#add").addEventListener("click",add);$("#add-empty").addEventListener("click",add);
$("#reload").addEventListener("click",loadRules);$("#refresh-entities").addEventListener("click",loadEntities);
$("#save").addEventListener("click",save);
$("#preview-form").addEventListener("submit",event=>{
  event.preventDefault();const input=$("#preview-input").value,key=normalize(input),result=$("#preview-result");
  if (!key)return;
  const matches=collect().filter(r=>r.phrases.some(p=>normalize(p)===key));
  if (matches.length>1)result.textContent="Фраза повторяется в нескольких командах. Уберите дубликаты перед сохранением.";
  else if (matches.length) {const rule=matches[0];result.textContent=`Home Assistant → ${rule.entity_id || "сущность не выбрана"} → ${rule.service==="turn_on"?"включить":"выключить"}.${dirty?" Это несохранённый список.":""}`;}
  else result.textContent="Запрос останется у Яндекса. В текущем списке нет такой фразы.";
  result.hidden=false;
});
window.addEventListener("beforeunload",event=>{if(dirty){event.preventDefault();event.returnValue="";}});
loadRules();loadEntities();
